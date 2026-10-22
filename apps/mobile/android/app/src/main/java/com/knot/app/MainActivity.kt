package com.knot.app

import com.facebook.react.ReactActivity
import com.facebook.react.ReactActivityDelegate
import com.facebook.react.defaults.DefaultNewArchitectureEntryPoint.fabricEnabled
import com.facebook.react.defaults.DefaultReactActivityDelegate

class MainActivity : ReactActivity() {

  /**
   * Returns the name of the main component registered from JavaScript. This is used to schedule
   * rendering of the component.
   */
  override fun getMainComponentName(): String = "Knot"

  /**
   * Returns the instance of the [ReactActivityDelegate]. We use [DefaultReactActivityDelegate]
   * which allows you to enable New Architecture with a single boolean flags [fabricEnabled]
   */
  override fun createReactActivityDelegate(): ReactActivityDelegate =
      DefaultReactActivityDelegate(this, mainComponentName, fabricEnabled)

  /**
   * Re-applies the app's navigation bar colour whenever the window focus moves.
   *
   * A React Native `Modal` is an Android `Dialog` with its own window, and that
   * window paints the navigation bar from RN's dialog theme, which cannot know the
   * app's theme choice. Opening a sheet therefore takes the focus away from this
   * Activity; dismiss the sheet and it comes back. Re-applying on both edges is
   * what keeps the bar in the active theme colour while a modal is open
   * (KNOT-020j-fix). It is a no-op until the theme has been reported once.
   */
  override fun onWindowFocusChanged(hasFocus: Boolean) {
    super.onWindowFocusChanged(hasFocus)
    SystemBarsModule.reapply(this)
  }
}
