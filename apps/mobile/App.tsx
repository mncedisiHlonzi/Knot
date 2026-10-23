import React, { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';
import {
  Animated,
  Appearance,
  BackHandler,
  Easing,
  SafeAreaView,
  StatusBar,
  StyleSheet,
  View,
  useWindowDimensions,
  type StatusBarProps,
} from 'react-native';

import type { AuthResponse, User } from './src/api/client';
import { conversationsApi } from './src/api/conversations';
import { Notification, notificationsApi } from './src/api/notifications';
import type { Story } from './src/api/stories';
import { versionsApi } from './src/api/versions';
import { DEFAULT_LANGUAGE_CODE } from './src/config/language';
import { isLanguageCode } from './src/data/languages';
import { motion, ThemeProvider, useTheme, useThemedStyles } from './src/theme';
import type { ThemeColors } from './src/theme';
import { setNavigationBarColor } from './src/native/systemBars';
import TabBar from './src/components/TabBar';
import type { TabName } from './src/components/TabBar';
import MediaGalleryModal from './src/components/MediaGalleryModal';
import type { ViewerMedia, ViewerStory } from './src/components/MediaGalleryModal';
import {
  popAllOverlays,
  popOverlay,
  pushOverlay,
  replaceOverlay,
  topOverlay,
} from './src/navigation/overlayStack';
import { hasSeenOnboarding, markOnboardingSeen } from './src/session/onboarding';
import { clearSession, loadSession, saveSession, Session } from './src/session/session';
import LoginScreen from './src/screens/auth/LoginScreen';
import RegisterScreen from './src/screens/auth/RegisterScreen';
import CommentThreadScreen from './src/screens/conversations/CommentThreadScreen';
import DiscoveryMapScreen from './src/screens/discovery/DiscoveryMapScreen';
import PlaceStoriesScreen from './src/screens/discovery/PlaceStoriesScreen';
import AskInquiryScreen from './src/screens/inquiries/AskInquiryScreen';
import InquiryDetailScreen from './src/screens/inquiries/InquiryDetailScreen';
import InquiryListScreen from './src/screens/inquiries/InquiryListScreen';
import NotificationsScreen from './src/screens/notifications/NotificationsScreen';
import OnboardingScreen from './src/screens/onboarding/OnboardingScreen';
import RootedSetupScreen from './src/screens/profile/RootedSetupScreen';
import BlockedUsersScreen from './src/screens/profile/BlockedUsersScreen';
import UserProfileScreen from './src/screens/profile/UserProfileScreen';
import SearchScreen from './src/screens/search/SearchScreen';
import SplashScreen from './src/screens/splash/SplashScreen';
import { SPLASH_DURATION_MS, postSplashDestination } from './src/screens/splash/splash';
import AdaptStoryScreen from './src/screens/stories/AdaptStoryScreen';
import CreateStoryScreen from './src/screens/stories/CreateStoryScreen';
import FeedScreen from './src/screens/stories/FeedScreen';
import LanguageTreeScreen from './src/screens/stories/LanguageTreeScreen';
import StoryDetailScreen from './src/screens/stories/StoryDetailScreen';
import StoryVersionScreen from './src/screens/stories/StoryVersionScreen';

/**
 * A non-tab screen, pushed over the tab bar.
 *
 * The primary destinations are tabs (see `TabName`); every other screen is an
 * overlay on top of whichever tab is active. Overlays form a stack (see
 * `overlayStack.ts`): each carries the ids it needs both to render and to open
 * the next screen, so back is always "pop the stack" rather than a hard-coded
 * target. Each variant's payload is compile-time checked, which a loose
 * `{ name, props }` shape would not give.
 */
/**
 * The system status bar, in the app's canvas colour with icons that contrast it.
 *
 * It is rendered by every root below rather than once, because the roots are
 * mutually exclusive phases — and React Native keeps a single status bar, so
 * whichever one is mounted is the one that applies. `translucent` is false so the
 * bar is a solid colour above the content rather than an overlay, which is what
 * the opaque canvas and the icons depend on. On iOS `backgroundColor` is
 * ignored — the platform has no bar to colour — and `barStyle` carries it, so the
 * same declaration is correct on both platforms. `StatusBar` cannot reach the
 * Android navigation bar; that is repainted at runtime from the active theme by
 * the small native module in `src/native/systemBars.ts` (KNOT-020j).
 */
function statusBarProps(colors: ThemeColors, theme: string): StatusBarProps {
  return {
    backgroundColor: colors.bg.canvas,
    // Dark ink on the light canvas, light ink on the dark one. In dark mode the
    // native theme's light-status-bar flag is overridden at runtime by this.
    barStyle: theme === 'dark' ? 'light-content' : 'dark-content',
    translucent: false,
  };
}

/** The push transition's curve: fast out, settling onto the screen. */
const EASING_ENTER = Easing.bezier(...motion.easing.enter);

/** The pop transition's curve: it accelerates away to the right. */
const EASING_EXIT = Easing.bezier(...motion.easing.exit);

type Overlay =
  | { readonly name: 'detail'; readonly storyId: string }
  | { readonly name: 'adapt'; readonly storyId: string; readonly parentVersionId: string }
  | { readonly name: 'tree'; readonly storyId: string }
  | { readonly name: 'storyVersion'; readonly versionId: string }
  | { readonly name: 'comments'; readonly storyId: string; readonly versionId: string }
  | { readonly name: 'rootedSetup' }
  | {
      readonly name: 'placeStories';
      readonly place: string;
      /**
       * The country the place's cluster was keyed by, or null when its stories
       * record none. It narrows the place page to that cluster's own stories, so
       * the page's list matches the story count in its header (KNOT-020e-fix2).
       */
      readonly country: string | null;
      /**
       * The cluster's coordinate (the average of its stories' points), or null when
       * it has none. It positions the place screen's decorative map strip, so the
       * strip costs no extra request; null means no strip is drawn
       * (KNOT-020e-fix3).
       */
      readonly latitude: number | null;
      readonly longitude: number | null;
      readonly storyCount: number;
      readonly pillarCounts: Readonly<Record<string, number>>;
    }
  | { readonly name: 'inquiry'; readonly inquiryId: string }
  | { readonly name: 'askInquiry' }
  | { readonly name: 'search' }
  | { readonly name: 'blockedUsers' }
  | { readonly name: 'userProfile'; readonly userId: string };

/** The two auth screens, shown before there is a session. Login is the default. */
type AuthMode = 'login' | 'register';

/** The boot flow: the splash, the first-run onboarding, then the app proper. */
type BootPhase = 'splash' | 'onboarding' | 'app';

/**
 * The first preferred language tag that is a canonical ISO 639-3 code, or
 * undefined when the profile names none the app knows. A legacy or malformed tag
 * is ignored rather than forwarded, because the server rejects it (KNOT-016-fix).
 */
function preferredLanguageTag(user: User): string | undefined {
  return user.preferred_languages.map((tag) => tag.trim()).find((tag) => isLanguageCode(tag));
}

/**
 * The first canonical preferred language, or the app-wide fallback. Used to seed
 * the language field when asking, adapting, and commenting.
 */
function preferredLanguage(user: User): string {
  return preferredLanguageTag(user) ?? DEFAULT_LANGUAGE_CODE;
}

/**
 * Root component for the Knot mobile app.
 *
 * The app has three pieces of navigation state: an active tab (Home, Map, Create,
 * Inquiries, Notifications), a stack of overlay screens pushed over it, and —
 * before there is a session — which auth screen is showing. The tab bar is
 * rendered only when the overlay stack is empty, so a secondary screen covers the
 * whole surface until it is popped. This is a hand-rolled state machine, not a
 * navigation library; see KNOT-ADR-019 and KNOT-ADR-025.
 *
 * The session is restored from AsyncStorage on launch and written back on login
 * or register, so it survives a restart; signing out clears it (KNOT-ADR-024).
 */
export default function App(): React.ReactElement {
  return (
    <ThemeProvider systemTheme={Appearance.getColorScheme()}>
      <ThemedApp />
    </ThemeProvider>
  );
}

/**
 * The app itself, under the theme provider.
 *
 * It sits inside the provider so its own stylesheet follows the active theme and
 * a theme change re-renders the whole tree — every screen reads its colours on
 * render, so nothing is left holding a stale palette (KNOT-020i).
 */
function ThemedApp(): React.ReactElement {
  const { colors, theme } = useTheme();
  const styles = useThemedStyles(createStyles);
  const STATUS_BAR = statusBarProps(colors, theme);
  const [session, setSession] = useState<Session | null>(null);
  const [authMode, setAuthMode] = useState<AuthMode>('login');
  const [tab, setTab] = useState<TabName>('feed');
  const [overlays, setOverlays] = useState<readonly Overlay[]>([]);
  // The unread notification count drawn on the bell tab. It is read here rather
  // than inside the bar so the bar stays stateless, and it is re-read whenever
  // the visible tab changes — which covers coming back from the inbox after it
  // marked rows read (KNOT-020j).
  const [unreadCount, setUnreadCount] = useState(0);

  // The full-screen media viewer. It lives at the root because it is a modal over
  // every screen — the Feed, a story, a version, a place, a profile — and because
  // a screen that opens it unmounts nothing: the viewer is a sibling of the app,
  // not a route (KNOT-020k).
  const [mediaViewer, setMediaViewer] = useState<{
    readonly items: readonly ViewerMedia[];
    readonly index: number;
    /**
     * The story the media belongs to, which the viewer's contextual sheet
     * presents. Null for a caller that has no story context (KNOT-020k-fix2).
     */
    readonly story: ViewerStory | null;
  } | null>(null);

  /**
   * Opens the viewer on one of `items`.
   *
   * The index is clamped here as well as in the viewer, because a caller can
   * hold a stale index after a list shrinks (KNOT-020k). The story context is
   * optional so a caller can still open the theatre alone.
   */
  const openMediaViewer = useCallback(
    (items: readonly ViewerMedia[], index: number, story?: ViewerStory | null): void => {
      if (items.length === 0) {
        return;
      }
      setMediaViewer({
        items,
        index: Math.min(Math.max(Math.round(index), 0), items.length - 1),
        story: story ?? null,
      });
    },
    [],
  );

  /**
   * Opens the story the viewer was showing.
   *
   * The viewer closes itself before calling this, so the push lands on a clean
   * stack and the reader returns to the feed (or the wall) when they come back
   * (KNOT-020k-fix2).
   */
  const handleViewStory = useCallback((storyId: string): void => {
    setOverlays((stack) => pushOverlay(stack, { name: 'detail', storyId }));
  }, []);

  // The Android navigation bar is not reachable from React Native's `StatusBar`:
  // the window paints it from the native theme, which cannot know the user's
  // theme choice. The small native module repaints it (and picks the icon
  // contrast) whenever the active theme changes, on every phase of the app —
  // splash, onboarding, auth, and the signed-in roots (KNOT-020j). On iOS this
  // is a no-op.
  useEffect(() => {
    setNavigationBarColor(colors.bg.canvas, theme === 'light');
  }, [colors, theme]);

  // The overlay transition (KNOT-020h).
  //
  // The app renders exactly one screen at a time, so a push slides the incoming
  // screen in from the right and a pop slides the leaving one back out over the
  // canvas. `screenOffset` is the only animated value: a translate on the screen
  // container, run on the native driver so the transition never drops a frame on
  // the JS thread.
  //
  // A push is detected by the stack getting deeper — every push site already goes
  // through `pushOverlay`, so the animation needs no per-call plumbing. A pop is
  // the reverse, but it is run BEFORE the state changes: the leaving screen stays
  // mounted and intact while it slides away, and the stack is only popped when the
  // animation finishes. `popping` makes a second back tap during those 280ms a
  // no-op, so one gesture can never pop two screens.
  const { width: windowWidth } = useWindowDimensions();
  const screenOffset = useRef(new Animated.Value(0)).current;
  const overlayDepth = useRef(0);
  const popping = useRef(false);
  // The boot flow. It starts on the splash, which is held while the stored session
  // and the first-run flag are read and for a short minimum, then hands off.
  const [bootPhase, setBootPhase] = useState<BootPhase>('splash');
  const [sessionLoaded, setSessionLoaded] = useState(false);
  const [onboardingLoaded, setOnboardingLoaded] = useState(false);
  const [onboarded, setOnboarded] = useState(false);

  // Read the stored session and the first-run flag on first mount, in parallel.
  // Neither read throws: a missing or corrupted value resolves to "not signed in"
  // / "not onboarded", which is the safe default (KNOT-ADR-024).
  useEffect(() => {
    let active = true;

    void (async () => {
      const [stored, seen] = await Promise.all([loadSession(), hasSeenOnboarding()]);
      if (!active) {
        return;
      }
      if (stored !== null) {
        setSession(stored);
      }
      setOnboarded(seen);
      setSessionLoaded(true);
      setOnboardingLoaded(true);
    })();

    return () => {
      active = false;
    };
  }, []);

  // Hold the splash until both reads have resolved and the minimum time has
  // elapsed, then hand off to onboarding or the app. A session always wins:
  // onboarding ends at the auth screens, so a signed-in user has seen it.
  useEffect(() => {
    if (bootPhase !== 'splash' || !sessionLoaded || !onboardingLoaded) {
      return undefined;
    }
    const timer = setTimeout(() => {
      setBootPhase(postSplashDestination(session !== null, onboarded));
    }, SPLASH_DURATION_MS);
    return () => clearTimeout(timer);
  }, [bootPhase, onboarded, onboardingLoaded, session, sessionLoaded]);

  /** Leaves onboarding for the auth screens, recording that it has been seen. */
  function handleOnboardingDone(): void {
    void markOnboardingSeen();
    setOnboarded(true);
    setBootPhase('app');
  }

  // Reads the badge count. The count is supplementary: a failure leaves the badge
  // as it was rather than showing an error over a screen, because the inbox
  // itself still works.
  useEffect(() => {
    const token = session?.accessToken;
    if (token === undefined) {
      setUnreadCount(0);
      return undefined;
    }

    let active = true;
    void (async () => {
      try {
        const { count } = await notificationsApi.getUnreadCount(token);
        if (active) {
          setUnreadCount(count);
        }
      } catch {
        // Intentionally ignored: a badge is not worth an error state.
      }
    })();

    return () => {
      active = false;
    };
    // `overlays.length` re-reads the count when a pushed screen is popped, which
    // is when the bar becomes visible again (KNOT-020j).
  }, [session, tab, overlays.length]);

  /**
   * Pops the top overlay, sliding it out to the right first.
   *
   * The pop is committed only when the animation finishes, so the screen keeps
   * its content while it leaves instead of remounting into its loading state. The
   * canvas shows behind it; the app renders one screen at a time, and keeping the
   * lower screen mounted would restart hidden video playback (KNOT-020h, disclosed).
   */
  const handleBack = useCallback((): void => {
    if (popping.current || overlays.length === 0) {
      return;
    }

    popping.current = true;
    Animated.timing(screenOffset, {
      toValue: windowWidth,
      duration: motion.durations.screen,
      easing: EASING_EXIT,
      useNativeDriver: true,
    }).start(({ finished }) => {
      popping.current = false;
      // A push that landed mid-exit stops this timing; that screen has taken over
      // the offset, so the pop must not also fire.
      if (!finished) {
        return;
      }
      screenOffset.setValue(0);
      setOverlays((current) => popOverlay(current));
    });
  }, [overlays.length, screenOffset, windowWidth]);

  // The slide, in one place: a deeper stack is a push (enter), a shallower one
  // that we did not animate ourselves is a jump (sign-out, a screen swapped in
  // place), which lands instantly. A layout effect runs before the frame is
  // painted, so the incoming screen is never seen at its resting position first.
  useLayoutEffect(() => {
    const depth = overlays.length;

    if (depth > overlayDepth.current) {
      screenOffset.stopAnimation();
      screenOffset.setValue(windowWidth);
      Animated.timing(screenOffset, {
        toValue: 0,
        duration: motion.durations.screen,
        easing: EASING_ENTER,
        useNativeDriver: true,
      }).start();
    } else if (depth < overlayDepth.current) {
      screenOffset.stopAnimation();
      screenOffset.setValue(0);
    }

    overlayDepth.current = depth;
  }, [overlays.length, screenOffset, windowWidth]);

  // Android hardware back pops the overlay stack and falls through to the OS
  // (background the app) only when there is nothing to pop. BackHandler exists on
  // every platform but only fires on Android, so this is inert on iOS.
  useEffect(() => {
    const subscription = BackHandler.addEventListener('hardwareBackPress', () => {
      if (overlays.length === 0) {
        return false;
      }
      handleBack();
      return true;
    });

    return () => subscription.remove();
  }, [handleBack, overlays.length]);

  function handleAuthenticated(result: AuthResponse): void {
    const next: Session = {
      accessToken: result.access_token,
      refreshToken: result.refresh_token,
      user: result.user,
    };
    void saveSession(next);
    setSession(next);
    setTab('feed');
    setOverlays((current) => popAllOverlays(current));
  }

  function handleSignOut(): void {
    void clearSession();
    setSession(null);
    setOverlays((current) => popAllOverlays(current));
    setTab('feed');
    setAuthMode('login');
  }

  function handleStoryCreated(story: Story): void {
    // Show the story the server actually stored, rather than assuming the form's
    // contents are what was persisted.
    setOverlays((current) => pushOverlay(current, { name: 'detail', storyId: story.id }));
  }

  /**
   * Replaces the session's user after a profile change (an avatar upload). The
   * updated copy is written back to storage so it survives a restart.
   */
  function handleUserUpdated(user: User): void {
    setSession((current) => {
      if (current === null) {
        return current;
      }
      const next: Session = { ...current, user };
      void saveSession(next);
      return next;
    });
  }

  /**
   * Renders the active tab's screen.
   *
   * The session is passed in rather than read from state so the type checker can
   * see that it is non-null here.
   */
  function renderTab(current: Session): React.ReactElement {
    switch (tab) {
      case 'discoveryMap':
        return (
          <DiscoveryMapScreen
            token={current.accessToken}
            onOpenPlace={(cluster) =>
              setOverlays((stack) =>
                pushOverlay(stack, {
                  name: 'placeStories',
                  place: cluster.place,
                  country: cluster.place_country,
                  latitude: cluster.latitude,
                  longitude: cluster.longitude,
                  storyCount: cluster.story_count,
                  pillarCounts: cluster.pillar_counts,
                }),
              )
            }
          />
        );
      case 'createStory':
        return (
          <CreateStoryScreen
            token={current.accessToken}
            onCreated={handleStoryCreated}
            onCancel={() => setTab('feed')}
          />
        );
      case 'inquiries':
        // The Inquiries tab is a destination, not a pushed screen: it lists the
        // open questions and is where asking starts. Asking and opening a question
        // are overlays, so the tab bar disappears while either is showing
        // (KNOT-ADR-058).
        return (
          <InquiryListScreen
            token={current.accessToken}
            onOpenInquiry={(inquiryId) =>
              setOverlays((stack) => pushOverlay(stack, { name: 'inquiry', inquiryId }))
            }
            onAsk={() => setOverlays((stack) => pushOverlay(stack, { name: 'askInquiry' }))}
            onOpenUserProfile={openUserProfile}
          />
        );
      case 'notifications':
        // Notifications is the fifth destination (KNOT-020j). It is rendered as
        // a tab rather than pushed over the bar because it is a primary
        // destination — the same treatment Inquiries receives — so the bar stays
        // visible and the tab reads as selected. The back affordance is therefore
        // omitted: the bar is how you leave it.
        return (
          <NotificationsScreen
            token={current.accessToken}
            onOpenEntity={handleOpenNotification}
            onBack={() => setTab('feed')}
            showBackButton={false}
          />
        );
      case 'feed':
      default:
        return (
          <FeedScreen
            token={current.accessToken}
            viewer={{
              display_name: current.user.display_name,
              avatar_url: current.user.avatar_url,
            }}
            onOpenStory={(id) =>
              setOverlays((stack) => pushOverlay(stack, { name: 'detail', storyId: id }))
            }
            // An adaptation entry opens the VERSION, not the story it adapts, so the
            // reader lands on the version they tapped (KNOT-022). It pushes the same
            // overlay the language tree uses for one version.
            onOpenVersion={(versionId) =>
              setOverlays((stack) => pushOverlay(stack, { name: 'storyVersion', versionId }))
            }
            onOpenSearch={() => setOverlays((stack) => pushOverlay(stack, { name: 'search' }))}
            onOpenProfile={() => openUserProfile(current.user.id)}
            onOpenUserProfile={openUserProfile}
            onOpenMedia={openMediaViewer}
          />
        );
    }
  }

  /**
   * Opens what a notification points at.
   *
   * Each entity type resolves to a screen over one or two public GETs:
   *
   *   version.created  GET /versions/{id}                 -> the story detail
   *   comment.created  GET /comments/{id}                 -> the comment's thread
   *   inquiry.*        the inquiry itself                    -> the inquiry detail
   *
   * `GET /comments/{id}` names both the version and the story, so a resolved
   * comment opens CommentThreadScreen with no further lookup. Both inquiry events
   * point at the inquiry rather than at the answer, so a tap opens the question,
   * where the new answer is visible in its thread (KNOT-ADR-056). The read marker is
   * written by the inbox before this runs, so this method only navigates.
   * Content that can no longer be read (deleted, offline) simply does not open;
   * the inbox stays where it is.
   */
  function handleOpenNotification(notification: Notification): void {
    void (async () => {
      // The token lets the server hide an entity authored by someone the viewer
      // has blocked (or who has blocked the viewer) (KNOT-ADR-064).
      const token = session?.accessToken;
      try {
        switch (notification.entity_type) {
          case 'story': {
            // A reaction on a story: the entity id names the story itself.
            setOverlays((stack) =>
              pushOverlay(stack, { name: 'detail', storyId: notification.entity_id }),
            );
            return;
          }
          case 'version': {
            const { version } = await versionsApi.getVersion(notification.entity_id, token);
            setOverlays((stack) =>
              pushOverlay(stack, { name: 'detail', storyId: version.story_id }),
            );
            return;
          }
          case 'comment': {
            const { comment } = await conversationsApi.getComment(notification.entity_id, token);
            openCommentThread(comment.version_id, comment.story_id);
            return;
          }
          case 'inquiry': {
            // Both inquiry.answered and inquiry.nearby name the inquiry, so a tap
            // opens the question with no conditional on the event type.
            setOverlays((stack) =>
              pushOverlay(stack, { name: 'inquiry', inquiryId: notification.entity_id }),
            );
            return;
          }
          default:
            // A notification the inbox does not produce today has nowhere to go.
            return;
        }
      } catch {
        // A version, comment, or inquiry that can no longer be read does not
        // open.
      }
    })();
  }

  /** Pushes the comment thread for a resolved version, over the inbox. */
  function openCommentThread(versionId: string, storyId: string): void {
    setOverlays((stack) => pushOverlay(stack, { name: 'comments', storyId, versionId }));
  }

  /**
   * Pushes a user's profile wall over whatever is showing.
   *
   * The same overlay serves the owner and everyone else: the screen shows the
   * owner controls only when the id is the signed-in user's, so tapping your own
   * author line opens your wall with the avatar and sign-out controls.
   */
  function openUserProfile(userId: string): void {
    setOverlays((stack) => pushOverlay(stack, { name: 'userProfile', userId }));
  }

  /**
   * Renders the top overlay over the active tab, or the tab itself when the stack
   * is empty.
   *
   * Every overlay's back action is the same: pop the stack, revealing whatever was
   * underneath — the tab, or the screen that pushed it. Advancing pushes a new
   * overlay; a screen that hands off in place (adapting a story, then its tree)
   * replaces its own entry via `replaceOverlay`.
   */
  function renderOverlay(current: Session): React.ReactElement {
    const overlay = topOverlay(overlays);

    if (overlay === undefined) {
      return renderTab(current);
    }

    switch (overlay.name) {
      case 'detail':
        return (
          <StoryDetailScreen
            id={overlay.storyId}
            token={current.accessToken}
            onBack={handleBack}
            onAdapt={(parentVersionId) =>
              setOverlays((stack) =>
                pushOverlay(stack, { name: 'adapt', storyId: overlay.storyId, parentVersionId }),
              )
            }
            onViewTree={() =>
              setOverlays((stack) => pushOverlay(stack, { name: 'tree', storyId: overlay.storyId }))
            }
            onConversation={(versionId) =>
              setOverlays((stack) =>
                pushOverlay(stack, { name: 'comments', storyId: overlay.storyId, versionId }),
              )
            }
            onOpenUserProfile={openUserProfile}
            onOpenMedia={openMediaViewer}
          />
        );
      case 'adapt':
        return (
          <AdaptStoryScreen
            storyId={overlay.storyId}
            parentVersionId={overlay.parentVersionId}
            token={current.accessToken}
            defaultLanguage={preferredLanguage(current.user)}
            onAdapted={() =>
              setOverlays((stack) =>
                replaceOverlay(stack, { name: 'tree', storyId: overlay.storyId }),
              )
            }
            onCancel={handleBack}
          />
        );
      case 'tree':
        return (
          <LanguageTreeScreen
            storyId={overlay.storyId}
            token={current.accessToken}
            onOpenUserProfile={openUserProfile}
            onOpenVersion={(versionId) =>
              setOverlays((stack) => pushOverlay(stack, { name: 'storyVersion', versionId }))
            }
            onTellStory={(parentVersionId) =>
              setOverlays((stack) =>
                pushOverlay(stack, { name: 'adapt', storyId: overlay.storyId, parentVersionId }),
              )
            }
            onBack={handleBack}
          />
        );
      case 'storyVersion':
        return (
          <StoryVersionScreen
            versionId={overlay.versionId}
            token={current.accessToken}
            onBack={handleBack}
            onAdapt={(storyId, parentVersionId) =>
              setOverlays((stack) =>
                pushOverlay(stack, { name: 'adapt', storyId, parentVersionId }),
              )
            }
            onOpenStory={(storyId) =>
              setOverlays((stack) => pushOverlay(stack, { name: 'detail', storyId }))
            }
            onConversation={(storyId, versionId) =>
              setOverlays((stack) => pushOverlay(stack, { name: 'comments', storyId, versionId }))
            }
            onViewTree={(storyId) =>
              setOverlays((stack) => pushOverlay(stack, { name: 'tree', storyId }))
            }
            onOpenUserProfile={openUserProfile}
            onOpenMedia={openMediaViewer}
          />
        );
      case 'comments':
        return (
          <CommentThreadScreen
            versionId={overlay.versionId}
            token={current.accessToken}
            language={preferredLanguageTag(current.user)}
            onOpenUserProfile={openUserProfile}
            onBack={handleBack}
          />
        );
      case 'rootedSetup':
        return (
          <RootedSetupScreen
            token={current.accessToken}
            onSaved={handleBack}
            onCancel={handleBack}
          />
        );
      case 'placeStories':
        return (
          <PlaceStoriesScreen
            place={overlay.place}
            country={overlay.country}
            latitude={overlay.latitude}
            longitude={overlay.longitude}
            storyCount={overlay.storyCount}
            pillarCounts={overlay.pillarCounts}
            token={current.accessToken}
            onOpenStory={(id) =>
              setOverlays((stack) => pushOverlay(stack, { name: 'detail', storyId: id }))
            }
            onOpenMedia={openMediaViewer}
            onBack={handleBack}
          />
        );
      case 'inquiry':
        return (
          <InquiryDetailScreen
            inquiryId={overlay.inquiryId}
            token={current.accessToken}
            preferredLanguages={current.user.preferred_languages}
            onOpenUserProfile={openUserProfile}
            onBack={handleBack}
          />
        );
      case 'askInquiry':
        return (
          <AskInquiryScreen
            token={current.accessToken}
            defaultLanguage={preferredLanguage(current.user)}
            onAsked={(inquiryId) =>
              setOverlays((stack) => replaceOverlay(stack, { name: 'inquiry', inquiryId }))
            }
            onCancel={handleBack}
          />
        );
      case 'search':
        return (
          <SearchScreen
            token={current.accessToken}
            onOpenStory={(storyId) =>
              setOverlays((stack) => pushOverlay(stack, { name: 'detail', storyId }))
            }
            onOpenVersionTree={(storyId) =>
              setOverlays((stack) => pushOverlay(stack, { name: 'tree', storyId }))
            }
            onOpenInquiry={(inquiryId) =>
              setOverlays((stack) => pushOverlay(stack, { name: 'inquiry', inquiryId }))
            }
            onOpenUserProfile={openUserProfile}
            onBack={handleBack}
          />
        );
      case 'blockedUsers':
        return <BlockedUsersScreen token={current.accessToken} onBack={handleBack} />;
      case 'userProfile':
        return (
          <UserProfileScreen
            userId={overlay.userId}
            token={current.accessToken}
            isOwnProfile={overlay.userId === current.user.id}
            onOpenStory={(storyId) =>
              setOverlays((stack) => pushOverlay(stack, { name: 'detail', storyId }))
            }
            onOpenComment={(storyId, versionId) =>
              setOverlays((stack) => pushOverlay(stack, { name: 'comments', storyId, versionId }))
            }
            onOpenInquiry={(inquiryId) =>
              setOverlays((stack) => pushOverlay(stack, { name: 'inquiry', inquiryId }))
            }
            onUserUpdated={handleUserUpdated}
            onSetRooted={() => setOverlays((stack) => pushOverlay(stack, { name: 'rootedSetup' }))}
            onOpenBlockedUsers={() =>
              setOverlays((stack) => pushOverlay(stack, { name: 'blockedUsers' }))
            }
            onOpenMedia={openMediaViewer}
            onSignOut={handleSignOut}
            onBack={handleBack}
          />
        );
      default:
        return renderTab(current);
    }
  }

  if (bootPhase === 'splash') {
    return (
      <SafeAreaView style={styles.bootContainer}>
        <StatusBar {...STATUS_BAR} />
        <SplashScreen />
      </SafeAreaView>
    );
  }

  if (bootPhase === 'onboarding') {
    return (
      <SafeAreaView style={styles.bootContainer}>
        <StatusBar {...STATUS_BAR} />
        <OnboardingScreen onDone={handleOnboardingDone} />
      </SafeAreaView>
    );
  }

  if (session === null) {
    return (
      <SafeAreaView style={styles.authContainer}>
        <StatusBar {...STATUS_BAR} />
        {authMode === 'login' ? (
          <LoginScreen
            onAuthenticated={handleAuthenticated}
            onSwitchToRegister={() => setAuthMode('register')}
          />
        ) : (
          <RegisterScreen
            onAuthenticated={handleAuthenticated}
            onSwitchToLogin={() => setAuthMode('login')}
          />
        )}
      </SafeAreaView>
    );
  }

  return (
    <SafeAreaView style={styles.storyContainer}>
      <StatusBar {...STATUS_BAR} />
      <View style={styles.body}>
        <Animated.View style={[styles.screen, { transform: [{ translateX: screenOffset }] }]}>
          {renderOverlay(session)}
        </Animated.View>
      </View>
      {overlays.length === 0 ? (
        <TabBar activeTab={tab} onSelect={setTab} unreadCount={unreadCount} />
      ) : null}

      {/* The viewer is a Modal, so it draws above the bar and every overlay. It is
          declared once for the signed-in app; opening it sets `mediaViewer`, and
          the viewer fades itself in and out (KNOT-020k). It carries the story's
          context into its bottom overlay and can hand the reader to the story
          itself (KNOT-020k-fix3). */}
      <MediaGalleryModal
        visible={mediaViewer !== null}
        media={mediaViewer?.items ?? []}
        initialIndex={mediaViewer?.index ?? 0}
        story={mediaViewer?.story ?? null}
        onClose={() => setMediaViewer(null)}
        onOpenStory={handleViewStory}
      />
    </SafeAreaView>
  );
}

const createStyles = (colors: ThemeColors) =>
  StyleSheet.create({
    authContainer: {
      backgroundColor: colors.bg.canvas,
      flex: 1,
    },
    body: {
      flex: 1,
      // The transition slides a screen past the window's edge; without this the
      // moving screen would paint outside the body while it leaves (KNOT-020h).
      overflow: 'hidden',
    },
    bootContainer: {
      backgroundColor: colors.bg.canvas,
      flex: 1,
    },
    screen: {
      flex: 1,
    },
    storyContainer: {
      backgroundColor: colors.bg.canvas,
      flex: 1,
    },
  });
