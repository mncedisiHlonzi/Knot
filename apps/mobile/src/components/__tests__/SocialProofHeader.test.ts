import type { RecentInteraction } from '../../api/stories';
import { spacing, typeScale } from '../../theme';
import Avatar from '../Avatar';
import SocialProofHeader, { type SocialProofAvatar } from '../SocialProofHeader';

// The header renders React Native views and the shared Avatar; stub the native
// primitives so the Flow source is not parsed and the component can be called
// directly (this project has no renderer).
jest.mock('react-native', () => ({
  Image: 'Image',
  Pressable: 'Pressable',
  StyleSheet: { create: (styles: unknown) => styles },
  Text: 'Text',
  View: 'View',
}));

jest.mock('../../config/api', () => ({ API_BASE_URL: 'http://localhost:8080' }));

/** One interaction fixture. */
function interaction(overrides: Partial<RecentInteraction>): RecentInteraction {
  return {
    type: 'reaction',
    actor_display_name: 'Tycoon',
    actor_avatar_url: '/users/tycoon/avatar?v=t.png',
    detail: 'rings_true',
    ...overrides,
  };
}

type ElementLike = { type?: unknown; props?: Record<string, unknown> };

/** Collects every element of a given type in a directly-called component's tree. */
function elementsByType(node: unknown, type: unknown, found: ElementLike[] = []): ElementLike[] {
  if (Array.isArray(node)) {
    for (const child of node) {
      elementsByType(child, type, found);
    }
    return found;
  }
  if (node === null || node === undefined || typeof node !== 'object') {
    return found;
  }

  const element = node as ElementLike;
  if (element.type === type) {
    found.push(element);
  }
  elementsByType(element.props?.children, type, found);

  return found;
}

/** Collects every string drawn anywhere in the tree. */
function collectText(node: unknown, found: string[] = []): string[] {
  if (typeof node === 'string') {
    found.push(node);
    return found;
  }
  if (Array.isArray(node)) {
    for (const child of node) {
      collectText(child, found);
    }
    return found;
  }
  if (node === null || node === undefined || typeof node !== 'object') {
    return found;
  }

  collectText((node as ElementLike).props?.children, found);
  return found;
}

/** Finds the first element carrying a testID. */
function findByTestId(node: unknown, testID: string): ElementLike | undefined {
  if (Array.isArray(node)) {
    for (const child of node) {
      const found = findByTestId(child, testID);
      if (found !== undefined) {
        return found;
      }
    }
    return undefined;
  }
  if (node === null || node === undefined || typeof node !== 'object') {
    return undefined;
  }

  const element = node as ElementLike;
  if (element.props?.testID === testID) {
    return element;
  }
  return findByTestId(element.props?.children, testID);
}

/** The lineage faces the attribution fallback draws. */
const lineageAvatars: readonly SocialProofAvatar[] = [
  {
    key: 'parent',
    uri: 'http://localhost:8080/users/parent/avatar?v=p.png',
    name: 'MaEnzy',
    label: "MaEnzy's profile picture",
  },
  {
    key: 'root',
    uri: 'http://localhost:8080/users/root/avatar?v=a.png',
    name: 'Ada Lovelace',
    label: "Ada Lovelace's profile picture",
  },
];

describe('SocialProofHeader', () => {
  it('draws the interactions and their avatars when there is social proof', () => {
    const element = SocialProofHeader({
      interactions: [
        interaction({ actor_display_name: 'Tycoon', detail: 'rings_true' }),
        interaction({
          type: 'comment',
          detail: null,
          actor_display_name: 'Yantuntu',
          actor_avatar_url: null,
        }),
      ],
      attributionLine: "Adapted from somebody else's original story.",
      lineageAvatars,
    });

    expect(findByTestId(element, 'social-proof-header')).toBeDefined();
    expect(collectText(element).join('')).toContain(
      'Tycoon finds this true and Yantuntu commented on this.',
    );

    const avatars = elementsByType(element, Avatar);
    expect(avatars).toHaveLength(2);
    expect(avatars.map((avatar) => avatar.props?.size)).toEqual(['xs', 'xs']);
    expect(avatars[0].props?.uri).toBe('http://localhost:8080/users/tycoon/avatar?v=t.png');
    // A missing avatar falls back to initials, not to an empty frame.
    expect(avatars[1].props?.uri).toBeUndefined();

    // The sentence keeps the body-small voice and the two-line clamp.
    const line = findByTestId(element, 'social-proof-sentence');
    expect(line?.props?.style).toContain(typeScale.caption);
    expect(line?.props?.numberOfLines).toBe(2);
  });

  it('draws one avatar for one actor', () => {
    const element = SocialProofHeader({
      interactions: [interaction({ actor_display_name: 'Tycoon' })],
    });

    expect(collectText(element).join('')).toContain('Tycoon finds this true.');
    expect(elementsByType(element, Avatar)).toHaveLength(1);
  });

  it('names one actor once when they both reacted and commented', () => {
    const element = SocialProofHeader({
      interactions: [
        interaction({ actor_display_name: 'Yantuntu', detail: 'rings_true' }),
        interaction({ type: 'comment', detail: null, actor_display_name: 'Yantuntu' }),
      ],
    });

    expect(collectText(element).join('')).toContain('Yantuntu finds this true and commented.');
    // One face and one name for the person, not two (KNOT-030).
    expect(elementsByType(element, Avatar)).toHaveLength(1);
  });

  it('renders for a story card when interactions exist and there is no attribution', () => {
    // A story has no hereditary sentence and no lineage avatars; the header must
    // still draw its interactions (KNOT-029).
    const element = SocialProofHeader({
      interactions: [interaction({ actor_display_name: 'Tycoon', detail: 'know_it_differently' })],
    });

    expect(element).not.toBeNull();
    expect(collectText(element).join('')).toContain('Tycoon knows this differently.');
    expect(elementsByType(element, Avatar)).toHaveLength(1);
  });

  it('falls back to the attribution sentence and its lineage faces', () => {
    const element = SocialProofHeader({
      interactions: [],
      attributionLine: "Adapted from Ada Lovelace's original story in English.",
      originalAuthorName: 'Ada Lovelace',
      lineageAvatars,
    });

    const text = collectText(element).join('');
    expect(text).toContain('Adapted from ');
    expect(text).toContain('Ada Lovelace');

    const avatars = elementsByType(element, Avatar);
    expect(avatars.map((avatar) => avatar.props?.uri)).toEqual([
      'http://localhost:8080/users/parent/avatar?v=p.png',
      'http://localhost:8080/users/root/avatar?v=a.png',
    ]);
    expect(findByTestId(element, 'attribution-divider')).toBeDefined();
  });

  it('builds the concise fallback sentence when the server sent none', () => {
    const element = SocialProofHeader({
      attributionLine: '',
      originalAuthorName: 'Ada Lovelace',
      lineageAvatars,
    });

    expect(collectText(element).join('')).toContain("Adapted from Ada Lovelace's original story.");
  });

  it('renders nothing when there are no interactions and nothing to attribute', () => {
    expect(
      SocialProofHeader({
        interactions: [],
        attributionLine: '',
        originalAuthorName: '',
      }),
    ).toBeNull();
  });

  it('renders nothing for an absent interactions array with no attribution', () => {
    expect(SocialProofHeader({})).toBeNull();
  });

  it('centres the credit in its band and keeps the rule clear of the sentence', () => {
    const element = SocialProofHeader({
      interactions: [interaction({ actor_display_name: 'Tycoon' })],
    });

    // The faces and the sentence are centred on each other inside one row.
    const band = findByTestId(element, 'social-proof-header');
    const row = (band?.props?.children as ElementLike[] | undefined)?.[0];
    const rowStyle = row?.props?.style as Record<string, unknown> | undefined;
    expect(rowStyle?.alignItems).toBe('center');
    expect(rowStyle?.flexDirection).toBe('row');

    // The card pads `spacing.sm` above its first child. The band repeats exactly
    // that step BELOW the row — between the sentence and the rule — so the credit
    // is centred between the card's top edge and the rule instead of sitting on it
    // and the band stays compact (KNOT-032-fix1, KNOT-032-fix2).
    const divider = findByTestId(element, 'attribution-divider');
    const dividerStyle = divider?.props?.style as Record<string, unknown> | undefined;
    expect(dividerStyle?.marginTop).toBe(spacing.sm);
    expect(dividerStyle?.marginBottom).toBeUndefined();

    // The rule keeps an equal clearance to the author block under it, so it closes
    // the band at the same tight rhythm and the header's total height is unchanged
    // (KNOT-032-fix2).
    const bandStyle = band?.props?.style as Record<string, unknown> | undefined;
    expect(bandStyle?.paddingBottom).toBe(spacing.sm);
    expect(bandStyle?.paddingTop).toBeUndefined();
  });
});
