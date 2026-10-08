/**
 * Knot mobile entry point.
 *
 * Registers the root component under the module name the native projects expect
 * ("Knot" — see ios/Knot/AppDelegate.mm and
 * android/app/src/main/java/com/knot/app/MainActivity.kt). The component itself
 * is App.tsx and is unchanged by the native scaffold.
 *
 * @format
 */

import { AppRegistry } from 'react-native';

import App from './App';

AppRegistry.registerComponent('Knot', () => App);
