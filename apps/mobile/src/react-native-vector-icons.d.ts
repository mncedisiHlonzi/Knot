/**
 * TypeScript declarations for the Ionicons entry point (KNOT-020k-fix3).
 *
 * `react-native-vector-icons@10` publishes JavaScript and Flow types but no
 * TypeScript declarations, so without this shim `tsc` fails the import with
 * TS7016 ("could not find a declaration file"). The shape below mirrors the
 * component `create-icon-set` returns: a text primitive that draws one glyph from
 * a bundled font, sized and tinted by props.
 *
 * Only the props the app passes are declared. `name` is deliberately `string`
 * here rather than a union of every Ionicons glyph, so this shim does not have to
 * be kept in step with the font; the tab bar narrows the names it draws, which is
 * where a typo can actually happen.
 *
 * Jest has no such shim: the bar's suite mocks the module with a string stand-in
 * (`jest.mock('react-native-vector-icons/Ionicons', …)`), because the package's
 * entry imports React Native's Flow source, which the node test environment
 * cannot parse.
 */
declare module 'react-native-vector-icons/Ionicons' {
  import type { ComponentType } from 'react';
  import type { StyleProp, TextStyle } from 'react-native';

  export type IoniconsProps = {
    /** The glyph's name in the bundled Ionicons font, e.g. `home-sharp`. */
    readonly name: string;
    /** The drawn size in points. */
    readonly size?: number;
    /** The glyph's ink. */
    readonly color?: string;
    readonly style?: StyleProp<TextStyle>;
  };

  const Ionicons: ComponentType<IoniconsProps>;
  export default Ionicons;
}
