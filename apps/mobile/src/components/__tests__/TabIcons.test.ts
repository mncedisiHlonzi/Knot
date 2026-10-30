import SvgStub from '../../assets/__mocks__/svgMock';
import { BellIcon, ChatIcon, HomeIcon, MapIcon, PlusIcon } from '../TabIcons';
import type { TabIconProps } from '../TabIcons';

/**
 * The bar's five glyphs are the founder's own SVG files (KNOT-020k-fix5), so
 * there is no drawing code to police. What matters is that each state draws the
 * right cut of the right file, at the size and in the ink the caller asked for.
 *
 * Jest has no Metro transform: `jest.config.js` maps every `.svg` specifier to one
 * shared stub, so a destination's two cuts are the same component object here. The
 * `testID` each component sets is what names the cut that was drawn, and that is
 * what these tests read.
 *
 * The components hold no state and call no hooks, so they are called directly, the
 * pattern the rest of the suite uses (there is no renderer, and this task may not
 * add one).
 */

type Element = {
  readonly type: unknown;
  readonly props: Record<string, unknown>;
};

/** Any ink; the glyph passes it straight through to the asset. */
const INK = '#1A1A1A';

/** The size the bar draws; the components must not invent one. */
const BAR_ICON_SIZE = 28;

type Destination = {
  readonly name: string;
  readonly Icon: (props: TabIconProps) => Element;
  /** The id of the idle (outline) cut. */
  readonly outline: string;
  /** The id of the selected (fill) cut. */
  readonly fill: string;
};

const DESTINATIONS: readonly Destination[] = [
  { name: 'home', Icon: HomeIcon, outline: 'tab-icon-home-outline', fill: 'tab-icon-home-fill' },
  { name: 'map', Icon: MapIcon, outline: 'tab-icon-map-outline', fill: 'tab-icon-map-fill' },
  { name: 'chat', Icon: ChatIcon, outline: 'tab-icon-chat-outline', fill: 'tab-icon-chat-fill' },
  { name: 'bell', Icon: BellIcon, outline: 'tab-icon-bell-outline', fill: 'tab-icon-bell-fill' },
];

describe('TabIcons', () => {
  it('draws the outline asset while idle and the fill asset while selected', () => {
    for (const destination of DESTINATIONS) {
      const idle = destination.Icon({ size: BAR_ICON_SIZE, color: INK, isActive: false });
      expect(`${destination.name}:${String(idle.props.testID)}`).toBe(
        `${destination.name}:${destination.outline}`,
      );

      const active = destination.Icon({ size: BAR_ICON_SIZE, color: INK, isActive: true });
      expect(`${destination.name}:${String(active.props.testID)}`).toBe(
        `${destination.name}:${destination.fill}`,
      );
    }
  });

  it('renders an SVG component in both states, not a drawn shape of its own', () => {
    for (const destination of DESTINATIONS) {
      for (const isActive of [false, true]) {
        const glyph = destination.Icon({ size: BAR_ICON_SIZE, color: INK, isActive });
        expect(glyph.type).toBe(SvgStub);
      }
    }
  });

  it('sizes and inks the asset from its props', () => {
    for (const destination of DESTINATIONS) {
      for (const isActive of [false, true]) {
        const glyph = destination.Icon({ size: BAR_ICON_SIZE, color: INK, isActive });
        expect(glyph.props.width).toBe(BAR_ICON_SIZE);
        expect(glyph.props.height).toBe(BAR_ICON_SIZE);
        expect(glyph.props.color).toBe(INK);
      }
    }
  });

  it('scales to whatever size the bar asks for', () => {
    const glyph = HomeIcon({ size: 20, color: INK, isActive: false });
    expect(glyph.props.width).toBe(20);
    expect(glyph.props.height).toBe(20);
  });

  it('hides the artwork from assistive technology', () => {
    for (const destination of DESTINATIONS) {
      const glyph = destination.Icon({ size: BAR_ICON_SIZE, color: INK, isActive: false });
      expect(glyph.props.accessible).toBe(false);
      expect(glyph.props.accessibilityElementsHidden).toBe(true);
      expect(glyph.props.importantForAccessibility).toBe('no-hide-descendants');
    }
  });

  // The supplied set has one plus, not a pair, so `isActive` draws nothing
  // different: the elevated disc behind the glyph carries the selection.
  it('draws the plus from its single cut in both states', () => {
    for (const isActive of [false, true]) {
      const plus = PlusIcon({ size: BAR_ICON_SIZE, color: INK, isActive });
      expect(plus.type).toBe(SvgStub);
      expect(plus.props.testID).toBe('tab-icon-plus');
      expect(plus.props.width).toBe(BAR_ICON_SIZE);
      expect(plus.props.height).toBe(BAR_ICON_SIZE);
      expect(plus.props.color).toBe(INK);
    }
  });
});
