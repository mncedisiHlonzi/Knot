package com.knot.app

import android.graphics.Color
import android.os.Build
import android.view.View
import android.view.WindowInsetsController

import com.facebook.react.bridge.ReactApplicationContext
import com.facebook.react.bridge.ReactContextBaseJavaModule
import com.facebook.react.bridge.ReactMethod
import com.facebook.react.bridge.UiThreadUtil

/**
 * Paints the Android system navigation bar in the app's active theme colour.
 *
 * The window paints the navigation bar from the native theme (`styles.xml`), and
 * that theme cannot know the user's theme choice: the choice is stored by the
 * JavaScript layer, where it can be either an explicit setting or the system
 * appearance. `StatusBar` in React Native does not reach the navigation bar
 * either. So the app calls this module whenever the active theme changes and the
 * bar is repainted here (KNOT-020j).
 *
 * Two properties are set:
 *
 * - **the bar colour**, so it matches `colors.bg.canvas` in either theme;
 * - **the icon appearance**, so the clock-free gesture pill and the three-button
 *   icons stay legible: dark icons on the light canvas, light icons on the dark
 *   one.
 *
 * `isNavigationBarContrastEnforced` is switched off on API 29+, where Android
 * would otherwise paint its own translucent scrim behind the bar and defeat the
 * colour just set.
 *
 * The call is a no-op when there is no current activity (the app is in the
 * background); the next theme-driven call repaints it. The values handled here
 * are API-gated: the light-icon flag exists from API 26, and the insets
 * controller from API 30.
 */
class SystemBarsModule(reactContext: ReactApplicationContext) :
    ReactContextBaseJavaModule(reactContext) {

  override fun getName(): String = "KnotSystemBars"

  /**
   * Sets the navigation bar colour and its icon contrast.
   *
   * @param color the bar colour as `#RRGGBB` or `#AARRGGBB`, sent by JavaScript
   *   from the active theme's canvas token.
   * @param lightIcons true for dark icons (the light theme), false for light
   *   icons (the dark theme).
   */
  @ReactMethod
  fun setNavigationBar(color: String, lightIcons: Boolean) {
    val parsed: Int =
        try {
          Color.parseColor(color)
        } catch (error: IllegalArgumentException) {
          // A malformed colour is ignored rather than crashing the app; the bar
          // keeps whatever the native theme gave it.
          return
        }

    UiThreadUtil.runOnUiThread {
      val activity = currentActivity ?: return@runOnUiThread
      val window = activity.window ?: return@runOnUiThread

      @Suppress("DEPRECATION")
      window.navigationBarColor = parsed

      if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
        window.isNavigationBarContrastEnforced = false
      }

      if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
        val controller = window.insetsController ?: return@runOnUiThread
        val mask = WindowInsetsController.APPEARANCE_LIGHT_NAVIGATION_BARS
        controller.setSystemBarsAppearance(if (lightIcons) mask else 0, mask)
      } else if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
        @Suppress("DEPRECATION")
        val flags = window.decorView.systemUiVisibility
        @Suppress("DEPRECATION")
        window.decorView.systemUiVisibility =
            if (lightIcons) {
              flags or View.SYSTEM_UI_FLAG_LIGHT_NAVIGATION_BAR
            } else {
              flags and View.SYSTEM_UI_FLAG_LIGHT_NAVIGATION_BAR.inv()
            }
      }
    }
  }
}
