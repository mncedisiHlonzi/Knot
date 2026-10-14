import type { Notification } from '../../../api/notifications';
import { API_BASE_URL } from '../../../config/api';
import { avatarUri, notificationParts, reactionPhrase } from '../notificationFormat';

/**
 * The inbox sentence is everything a notification row says, so its rules are
 * pinned here rather than left to a screen render: this project has no renderer,
 * and the sentence split is the one piece of logic the redesign added (KNOT-020f).
 */
function notification(overrides: Partial<Notification> = {}): Notification {
  return {
    actor: {
      avatar_url: '/users/u1/avatar?v=1',
      display_name: 'Luis M.',
      id: 'u1',
    },
    created_at: '2026-01-01T00:00:00Z',
    entity_id: 'e1',
    entity_type: 'version',
    event_type: 'version.created',
    id: 'n1',
    read: false,
    ...overrides,
  };
}

describe('notificationParts', () => {
  it('names the actor and the act for an adaptation', () => {
    expect(notificationParts(notification())).toEqual({
      actor: 'Luis M.',
      text: 'adapted your version.',
    });
  });

  it('names "Someone" when the actor no longer resolves', () => {
    expect(notificationParts(notification({ actor: null }))).toEqual({
      actor: 'Someone',
      text: 'adapted your version.',
    });
  });

  it('names the reaction signal inside the clause', () => {
    expect(
      notificationParts(
        notification({
          entity_type: 'story',
          event_type: 'reaction.created',
          reaction_type: 'rings_true',
        }),
      ),
    ).toEqual({ actor: 'Luis M.', text: '✅ Rings true on your story.' });
  });

  it('degrades a signal this build does not know to "reacted"', () => {
    expect(
      notificationParts(
        notification({ event_type: 'reaction.created', reaction_type: 'not_a_signal' }),
      ),
    ).toEqual({ actor: 'Luis M.', text: 'reacted on your version.' });
  });

  it('reads a nearby inquiry as being about the recipient\u2019s place', () => {
    expect(
      notificationParts(notification({ entity_type: 'inquiry', event_type: 'inquiry.nearby' })),
    ).toEqual({ actor: 'Luis M.', text: 'asked a question about your place.' });
  });

  it('falls back to a generic clause for an event this build does not know', () => {
    // A newer server may add an event before this build learns it. The cast is the
    // point of the test: the type says this cannot happen, and the sentence must
    // still read.
    const unknown = { ...notification(), event_type: 'story.deleted' } as unknown as Notification;

    expect(notificationParts(unknown)).toEqual({
      actor: 'Luis M.',
      text: 'did something on your content.',
    });
  });
});

describe('reactionPhrase', () => {
  it('draws the emoji and the label for a known signal', () => {
    expect(reactionPhrase('needs_a_source')).toBe('📎 Needs a source');
  });

  it('returns the generic phrase for an absent signal', () => {
    expect(reactionPhrase(undefined)).toBe('reacted');
  });
});

describe('avatarUri', () => {
  it('returns nothing for an actor with no avatar', () => {
    expect(avatarUri('')).toBeUndefined();
    expect(avatarUri('   ')).toBeUndefined();
  });

  it('resolves a path against the API base URL', () => {
    expect(avatarUri('/users/u1/avatar?v=1')).toBe(`${API_BASE_URL}/users/u1/avatar?v=1`);
  });

  it('trims a padded path before resolving it', () => {
    expect(avatarUri('  /users/u1/avatar  ')).toBe(`${API_BASE_URL}/users/u1/avatar`);
  });
});
