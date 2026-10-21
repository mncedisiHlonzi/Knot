// Command knot is the entry point for the Knot backend.
//
// It is the composition root: it loads configuration, builds the structured
// logger, connects to PostgreSQL, assembles the store -> service -> handler graph
// explicitly (there is no dependency-injection framework), and serves the HTTP
// API. It is the only place that knows how the layers are wired together.
//
// Usage:
//
//	knot              start the HTTP API
//	knot migrate up   apply pending database migrations, then exit
//
// Migrations are deliberately NOT run automatically on server start: schema
// changes stay an explicit, reviewable action.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/knot/backend/internal/appinfo"
	"github.com/knot/backend/internal/config"
	"github.com/knot/backend/internal/conversations"
	"github.com/knot/backend/internal/discovery"
	"github.com/knot/backend/internal/httpapi"
	"github.com/knot/backend/internal/identity"
	"github.com/knot/backend/internal/inquiries"
	"github.com/knot/backend/internal/interactions"
	"github.com/knot/backend/internal/moderation"
	"github.com/knot/backend/internal/notifications"
	"github.com/knot/backend/internal/profile"
	"github.com/knot/backend/internal/reactions"
	"github.com/knot/backend/internal/rooted"
	"github.com/knot/backend/internal/search"
	"github.com/knot/backend/internal/storage"
	"github.com/knot/backend/internal/stories"
	"github.com/knot/backend/internal/storymedia"
	"github.com/knot/backend/internal/storystats"
	"github.com/knot/backend/internal/versions"
	"github.com/knot/backend/migrations"
)

// Timeouts used at startup and shutdown.
const (
	// startupTimeout bounds the initial database connection.
	startupTimeout = 10 * time.Second
	// migrateTimeout bounds a whole migration run.
	migrateTimeout = 2 * time.Minute
	// shutdownGrace is how long in-flight requests get to finish.
	shutdownGrace = 10 * time.Second
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		// The logger is built inside run, so report failures on stderr.
		fmt.Fprintf(os.Stderr, "%s: %v\n", appinfo.Name, err)
		os.Exit(1)
	}
}

// run parses the subcommand and dispatches to it.
func run(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger := newLogger(cfg)
	if cfg.UsingInsecureJWTSecret() {
		logger.Warn(
			"using an insecure JWT secret; tokens are forgeable by anyone who can read it. Set KNOT_JWT_SECRET to at least 32 random bytes.",
			slog.String("env", cfg.Env),
		)
	}

	switch {
	case len(args) == 0:
		return serve(cfg, logger)
	case len(args) == 2 && args[0] == "migrate" && args[1] == "up":
		return migrateUp(cfg, logger)
	default:
		return fmt.Errorf("unknown command %q; usage: knot [migrate up]", args)
	}
}

// newLogger returns the structured logger for the configured environment: text
// for local development, JSON everywhere else.
func newLogger(cfg config.Config) *slog.Logger {
	level := slog.LevelInfo
	switch cfg.LogLevel {
	case config.LogLevelDebug:
		level = slog.LevelDebug
	case config.LogLevelWarn:
		level = slog.LevelWarn
	case config.LogLevelError:
		level = slog.LevelError
	}

	options := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	if cfg.Env == config.EnvLocal {
		handler = slog.NewTextHandler(os.Stdout, options)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, options)
	}

	return slog.New(handler)
}

// serve builds the application graph, starts the HTTP server, and blocks until a
// termination signal arrives, then drains in-flight requests.
func serve(cfg config.Config, logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := connect(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer pool.Close()

	store, err := identity.NewPostgresStore(pool)
	if err != nil {
		return err
	}

	tokens, err := identity.NewTokenIssuer(cfg.JWTSecret)
	if err != nil {
		return err
	}

	service, err := identity.NewService(store, tokens)
	if err != nil {
		return err
	}

	authHandler, err := httpapi.NewAuthHandler(service, logger)
	if err != nil {
		return err
	}

	// Rooted is Knot's trust and community layer. Its service doubles as the
	// enrichment lookup the content handlers use to attach an author's inline
	// Rooted summary to a response.
	rootedStore, err := rooted.NewPostgresStore(pool)
	if err != nil {
		return err
	}

	rootedService, err := rooted.NewService(rootedStore)
	if err != nil {
		return err
	}

	rootedHandler, err := httpapi.NewRootedHandler(rootedService, logger)
	if err != nil {
		return err
	}

	storiesStore, err := stories.NewPostgresStore(pool)
	if err != nil {
		return err
	}

	storiesService, err := stories.NewService(storiesStore)
	if err != nil {
		return err
	}

	// Media storage. Avatars and story media are written and read by this process
	// only: the bucket is private and no client is ever handed a link to it
	// (KNOT-ADR-028, KNOT-ADR-029). Building the client performs no I/O, so a
	// server that starts while the object store is down still answers everything
	// else, and the first upload is what reports that the store is unreachable.
	mediaStorage, err := storage.NewS3Storage(
		ctx,
		cfg.S3Endpoint,
		cfg.S3Region,
		cfg.S3AccessKey,
		cfg.S3SecretKey,
		cfg.S3Bucket,
	)
	if err != nil {
		return err
	}

	// Story media. The service owns the object store and the rows, so the stories
	// handler reuses the very same service as its media enrichment lookup.
	storyMediaStore, err := storymedia.NewPostgresStore(pool)
	if err != nil {
		return err
	}

	storyMediaService, err := storymedia.NewService(storyMediaStore, mediaStorage, logger)
	if err != nil {
		return err
	}

	// Reactions: the four perspective signals. The service is both the toggle
	// engine and the enrichment lookup the content handlers use to attach counts
	// and the reader's own signals to a response (KNOT-ADR-051). It reuses the
	// identity service as its batched actor lookup, exactly as the content
	// handlers do, so it is built before them.
	reactionsStore, err := reactions.NewPostgresStore(pool)
	if err != nil {
		return err
	}

	reactionsService, err := reactions.NewService(reactionsStore, service, logger)
	if err != nil {
		return err
	}

	// Story stats: the aggregate counts a story carries (versions, languages,
	// comments, media). They span the version, conversation, and media domains, so
	// they are a read model of their own rather than a query in any one store
	// (KNOT-020c-fix).
	storyStatsStore, err := storystats.NewPostgresStore(pool)
	if err != nil {
		return err
	}

	// Interactions: the newest reaction and comment on each feed entry, read in
	// two batched statements per page rather than one query per entry. Like the
	// stats store it spans the reaction and conversation domains, so it is a read
	// model of its own (KNOT-028).
	interactionsStore, err := interactions.NewPostgresStore(pool)
	if err != nil {
		return err
	}

	// The engagement log is the relevance engine's data pipeline: one weighted row
	// per view, read, reaction, comment and adaptation, written off the request
	// path through the same store and pool (KNOT-026).
	interactionService, err := interactions.NewService(interactionsStore)
	if err != nil {
		return err
	}

	// The identity service doubles as the batched author lookup every content
	// handler enriches with, so a response names and pictures its authors without
	// a query per row (KNOT-ADR-041).
	storyMediaHandler, err := httpapi.NewStoryMediaHandler(storyMediaService, logger)
	if err != nil {
		return err
	}

	// Notifications. The service is built before the content domains that call it,
	// because each of them is handed this same value as its one-method Notifier
	// hook: the notifications package is a leaf that never imports a content
	// domain (KNOT-ADR-040).
	notificationsStore, err := notifications.NewPostgresStore(pool)
	if err != nil {
		return err
	}

	notificationsService, err := notifications.NewService(notificationsStore, logger)
	if err != nil {
		return err
	}

	notificationsHandler, err := httpapi.NewNotificationsHandler(notificationsService, service, rootedService, logger)
	if err != nil {
		return err
	}

	versionsStore, err := versions.NewPostgresStore(pool)
	if err != nil {
		return err
	}

	versionsService, err := versions.NewService(versionsStore, notificationsService)
	if err != nil {
		return err
	}

	versionsHandler, err := httpapi.NewVersionsHandlerWithRecorder(versionsService, service, rootedService, reactionsService, logger, interactionService)
	if err != nil {
		return err
	}

	// The stories handler serves the feed, which lists stories and their
	// adaptations in one stream, so it is built after the versions service and
	// takes it as its chain lookup: attributing a page of adaptations is a batched
	// read of the version domain, not a query per entry (KNOT-022).
	storiesHandler, err := httpapi.NewStoriesHandlerWithRanking(
		storiesService,
		service,
		rootedService,
		storyMediaService,
		reactionsService,
		storyStatsStore,
		versionsService,
		interactionsStore,
		logger,
		interactionService,
		// The `for_you` feed reads the reader's profile, their weighted
		// engagement per pillar, and the entity_features read model that gives
		// each candidate its language, pillar and place (KNOT-027).
		httpapi.FeedRanking{
			Profiles:   service,
			Engagement: interactionsStore,
			Features:   storiesStore,
		},
	)
	if err != nil {
		return err
	}

	// The conversations store backs the comment service. Comments and their
	// versions live in the same database and the same bounded context.
	conversationsStore, err := conversations.NewPostgresStore(pool)
	if err != nil {
		return err
	}

	conversationsService, err := conversations.NewService(conversationsStore, notificationsService)
	if err != nil {
		return err
	}

	conversationsHandler, err := httpapi.NewConversationsHandlerWithRecorder(conversationsService, service, rootedService, reactionsService, logger, interactionService)
	if err != nil {
		return err
	}

	// Curious Inquiries. Asking a question routes it to the first Rooted users of
	// the place it names, so the service is handed the rooted service as its
	// routing lookup — the same value the content handlers use for enrichment —
	// and the notifications service as its two-method Notifier hook (KNOT-ADR-056).
	inquiriesStore, err := inquiries.NewPostgresStore(pool)
	if err != nil {
		return err
	}

	inquiriesService, err := inquiries.NewService(inquiriesStore, rootedService, notificationsService, logger)
	if err != nil {
		return err
	}

	inquiriesHandler, err := httpapi.NewInquiriesHandler(inquiriesService, service, rootedService, reactionsService, logger)
	if err != nil {
		return err
	}

	// The reactions handler owns the eight per-entity reaction routes. It uses the
	// three content services only to resolve an entity's author and confirm it
	// exists, and the notifications service to fire reaction.created (KNOT-ADR-050).
	reactionsHandler, err := httpapi.NewReactionsHandlerWithRecorder(reactionsService, storiesService, versionsService, conversationsService, inquiriesService, notificationsService, logger, interactionService)
	if err != nil {
		return err
	}

	// Discovery reads the stories and story_versions tables directly: finding
	// stories by place is a different read shape over the same data, so it owns
	// its own store rather than widening the stories interface. Its handler reuses
	// the same enrichment lookups the stories handler does — identity for
	// attribution, rooted for inline badges, story media for the preview, reactions
	// for the counts, and the stats store for the aggregates — so a story carries
	// identical fields from the feed and from a place (KNOT-020e-fix2).
	discoveryStore, err := discovery.NewPostgresStore(pool)
	if err != nil {
		return err
	}

	discoveryService, err := discovery.NewService(discoveryStore)
	if err != nil {
		return err
	}

	discoveryHandler, err := httpapi.NewDiscoveryHandler(discoveryService, service, rootedService, storyMediaService, reactionsService, storyStatsStore, logger)
	if err != nil {
		return err
	}

	avatarHandler, err := httpapi.NewAvatarHandler(service, mediaStorage, logger)
	if err != nil {
		return err
	}

	// The profile wall reads across the four content tables in one UNION ALL, so it
	// owns its own store. It reuses the identity service to resolve the wall's owner
	// and the rooted service to decorate the header (KNOT-ADR-042).
	profileStore, err := profile.NewPostgresStore(pool)
	if err != nil {
		return err
	}

	profileService, err := profile.NewService(service, profileStore)
	if err != nil {
		return err
	}

	profileHandler, err := httpapi.NewProfileHandler(profileService, rootedService, storyMediaService, logger)
	if err != nil {
		return err
	}

	// Moderation (KNOT-017a): blocks and reports. The entity lookup confirms a
	// reported entity exists through the content services built above, so the
	// moderation package never imports a content domain. The service is the
	// block lookup the auth middleware resolves once per authenticated request, and
	// the block/report handler's backend.
	moderationStore, err := moderation.NewPostgresStore(pool)
	if err != nil {
		return err
	}

	entityExistence, err := httpapi.NewEntityExistence(storiesService, versionsService, conversationsService, inquiriesService, inquiriesService)
	if err != nil {
		return err
	}

	moderationService, err := moderation.NewService(moderationStore, entityExistence, logger)
	if err != nil {
		return err
	}

	moderationHandler, err := httpapi.NewModerationHandler(moderationService, service, logger)
	if err != nil {
		return err
	}

	// Search reads across stories, story_versions, and inquiries in one ranked
	// list, so — like the profile wall and discovery — it owns its own store
	// rather than widening three domain interfaces. Its handler reuses the identity
	// service for attribution and the rooted service for inline badges, exactly as
	// every other content handler does (KNOT-ADR-065).
	searchStore, err := search.NewPostgresStore(pool)
	if err != nil {
		return err
	}

	searchService, err := search.NewService(searchStore)
	if err != nil {
		return err
	}

	searchHandler, err := httpapi.NewSearchHandler(searchService, service, rootedService, logger)
	if err != nil {
		return err
	}

	// The same issuer that signs access tokens verifies them on protected
	// routes, so there is one source of truth for the signing key. The middleware
	// also resolves the caller's block set once per request and carries it on the
	// context, where the content stores filter a page from it (KNOT-ADR-060).
	authMiddleware, err := httpapi.NewAuthMiddleware(tokens, logger, httpapi.WithBlockLookup(moderationService))
	if err != nil {
		return err
	}

	router, err := httpapi.NewRouter(authHandler, storiesHandler, versionsHandler, conversationsHandler, rootedHandler, discoveryHandler, avatarHandler, storyMediaHandler, notificationsHandler, profileHandler, reactionsHandler, inquiriesHandler, moderationHandler, searchHandler, authMiddleware, appinfo.Version, logger)
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler: router.Handler(),
		// These bounds protect the server from slow or stalled clients.
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info(fmt.Sprintf("%s %s starting (env=%s, port=%d)", appinfo.Name, appinfo.Version, cfg.Env, cfg.HTTPPort))
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
		}
	}()

	select {
	case err := <-serverErrors:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
		logger.Info("termination signal received; draining connections", slog.Duration("grace", shutdownGrace))
	}

	// The signal context is already cancelled, so shutdown needs a fresh one.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}

	logger.Info("shutdown complete")

	return nil
}

// migrateUp applies every pending migration and exits.
func migrateUp(cfg config.Config, logger *slog.Logger) error {
	ctx, cancel := context.WithTimeout(context.Background(), migrateTimeout)
	defer cancel()

	pool, err := connect(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer pool.Close()

	applied, err := migrations.Up(ctx, pool)
	if err != nil {
		return err
	}

	if len(applied) == 0 {
		logger.Info("database schema is already up to date")
		return nil
	}

	logger.Info("migrations applied", slog.Any("versions", applied))

	return nil
}

// connect opens the pool and proves it can reach the database, so a bad DSN or a
// down database fails the process immediately rather than on the first request.
//
// The DSN is never logged: it can carry credentials.
func connect(ctx context.Context, cfg config.Config, logger *slog.Logger) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, cfg.PostgresDSN)
	if err != nil {
		return nil, fmt.Errorf("configure postgres pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, startupTimeout)
	defer cancel()

	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		logger.Error("could not reach postgres", slog.String("env", cfg.Env))
		return nil, fmt.Errorf("reach postgres: %w", err)
	}

	return pool, nil
}
