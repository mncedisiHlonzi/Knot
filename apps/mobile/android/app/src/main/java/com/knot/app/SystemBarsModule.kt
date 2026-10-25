package com.knot.app

import android.app.Activity
import android.graphics.Color
import android.os.Build
import android.view.View
import android.view.ViewGroup
import android.view.Window
import android.view.WindowInsetsController

import com.facebook.react.bridge.ReactApplicationContext
import com.facebook.react.bridge.ReactContextBaseJavaModule
import com.facebook.react.bridge.ReactMethod
import com.facebook.react.bridge.UiThreadUtil
import com.facebook.react.views.modal.ReactModalHostView

/**
 * Paints the Android system navigation bar in the app's active theme colour.
 *
 * The window paints the navigation bar from its theme, and no theme native side
 * can know the user's choice — it is stored by the JavaScript layer, where it is
 * either an explicit setting or the system appearance. `StatusBar` in React
 * Native does not reach the navigation bar either. So the app calls this module
 * whenever the active theme changes and the bar is repainted here (KNOT-020j).
 *
 * **Dialogs own the bar too.** React Native's `Modal` is a real Android `Dialog`
 * with its own window, created from RN's `Theme.FullScreenDialog*` — a theme with
 * no parent and no `navigationBarColor`, so while a modal is open the bar falls
 * back to the platform default and the Activity's colour is hidden underneath
 * (this was the dark-mode bug KNOT-020j-fix fixes). Every `ReactModalHostView`
 * stays attached to the React root while its dialog is up and exposes that
 * `Dialog`, so the same colour is applied to every open modal window:
 *
 * - whenever JavaScript sets a colour, which covers a theme change made while a
 *   sheet is open (the Theme row lives inside the profile sheet);
 * - whenever the window focus moves, which is what a dialog taking focus (and
 *   giving it back on dismissal) does — see `MainActivity.onWindowFocusChanged`.
 *
 * Two properties are set on each window:
 *
 * - **the bar colour**, so it matches `colors.bg.canvas` in either theme;
 * - **the icon appearance**, so the gesture pill and the three-button icons stay
 *   legible: dark icons on the light canvas, light icons on the dark one.
 *
 * `isNavigationBarContrastEnforced` is switched off on API 29+, where Android
 * would otherwise paint its own translucent scrim behind the bar and defeat the
 * colour just set.
 *
 * The call is a no-op when there is no current activity (the app is in the
 * background); the next focus change or theme-driven call repaints it. The
 * values handled here are API-gated: the light-icon flag exists from API 26, and
 * the insets controller from API 30.
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

    lastColor = parsed
    lastLightIcons = lightIcons

    UiThreadUtil.runOnUiThread {
      val activity = currentActivity ?: return@runOnUiThread
      reapply(activity)
    }
  }

  /**
   * Forces the status bar's icon contrast and its background colour, on the
   * Activity and on every open modal dialog.
   *
   * React Native's `StatusBar` reaches the Activity window only. A `Modal` is a
   * real Android `Dialog` with its own window, and React Native copies the
   * Activity's status-bar appearance into that window once, when the dialog is
   * shown — so a `<StatusBar barStyle="light-content" />` declared inside a modal
   * cannot turn the icons white on the dialog that is already up. The media
   * viewer needs white icons over its black room whether the page is an image or
   * a video, in either app theme (KNOT-020k-fix8).
   *
   * The background is painted here too, and that is the point of `background`: a
   * dialog window does not inherit the Activity's status-bar colour, so forcing
   * only the icons left the bar its theme colour — the app's canvas, white, on
   * every screen that had not already declared a transparent bar. Painting both
   * windows makes the viewer's chrome independent of the screen it opened from
   * (KNOT-020k-fix15).
   *
   * @param lightContent true for white icons on a dark surface, false for dark
   *   icons on a light one.
   * @param background the bar's fill, `#RRGGBB` or `#AARRGGBB`.
   */
  @ReactMethod
  fun setStatusBarIcons(lightContent: Boolean, background: String) {
    val parsed: Int =
        try {
          Color.parseColor(background)
        } catch (error: IllegalArgumentException) {
          // A malformed colour is ignored rather than crashing the app; the bar
          // keeps whatever the theme gave it.
          return
        }

    lastStatusBarLightContent = lightContent
    lastStatusBarColor = parsed

    UiThreadUtil.runOnUiThread {
      val activity = currentActivity ?: return@runOnUiThread
      reapplyStatusBar(activity)
    }
  }

  /**
   * Hands the status bar back to React Native's `StatusBar`.
   *
   * It is a separate method rather than a null argument because the bridge
   * cannot marshal a null into a `boolean` parameter (it would throw
   * `UnexpectedNativeTypeException`), so "stop managing" needs its own entry
   * point (KNOT-020k-fix8). The Activity's status bar is repainted in the chrome
   * colour JavaScript has already restored (the canvas), so the black does not
   * outlive the viewer (KNOT-020k-fix15).
   */
  @ReactMethod
  fun clearStatusBarIcons() {
    lastStatusBarLightContent = null
    lastStatusBarColor = null

    UiThreadUtil.runOnUiThread {
      val activity = currentActivity ?: return@runOnUiThread
      // `setNavigationBar` is called immediately before this on close, so `lastColor`
      // is the colour the app is returning to.
      val chrome = lastColor ?: return@runOnUiThread
      applyStatusBarBackground(activity.window, chrome)
    }
  }

  companion object {
    /**
     * The last colour JavaScript asked for. Held statically because the focus
     * callback lives on the Activity, and because a dialog window can appear
     * after the theme was chosen (KNOT-020j-fix).
     */
    private var lastColor: Int? = null
    private var lastLightIcons: Boolean = false

    /**
     * The forced status-bar contrast, or null when React Native's `StatusBar`
     * owns the bar again. Null is the resting state: only the media viewer forces
     * white icons, and it hands the bar back when it closes (KNOT-020k-fix8).
     */
    private var lastStatusBarLightContent: Boolean? = null

    /**
     * The viewer's status-bar fill, or null when React Native owns the bar. The
     * dialog window does not inherit the Activity's status-bar colour, so this has
     * to be painted on both (KNOT-020k-fix15).
     */
    private var lastStatusBarColor: Int? = null

    /**
     * Repaints the Activity's window and every open modal dialog.
     *
     * Called from `setNavigationBar` and from `MainActivity.onWindowFocusChanged`,
     * so a dialog that has just taken focus is corrected and the Activity is
     * corrected again when the dialog is dismissed. It must run on the UI thread;
     * both callers already do.
     */
    fun reapply(activity: Activity) {
      reapplyNavigationBar(activity)
      reapplyStatusBar(activity)
    }

    /** Repaints the navigation bar on the Activity and on every open modal. */
    private fun reapplyNavigationBar(activity: Activity) {
      val color = lastColor ?: return
      val decorView = activity.window?.decorView

      applyToWindow(activity.window, color, lastLightIcons)
      applyToOpenDialogs(decorView, color, lastLightIcons)
    }

    /**
     * Forces the status bar's icon contrast while the media viewer owns it.
     *
     * The dialog pass matters because the viewer's window copied the Activity's
     * appearance when it was created, before JavaScript's effect could run, so the
     * Activity alone would leave dark icons over the black room (KNOT-020k-fix8).
     */
    private fun reapplyStatusBar(activity: Activity) {
      val lightContent = lastStatusBarLightContent ?: return
      val color = lastStatusBarColor ?: return
      val decorView = activity.window?.decorView

      applyStatusBarToWindow(activity.window, lightContent, color)
      applyStatusBarToOpenDialogs(decorView, lightContent, color)
    }

    /** Sets the bar colour, contrast enforcement, and icon appearance on one window. */
    private fun applyToWindow(window: Window?, color: Int, lightIcons: Boolean) {
      if (window == null) {
        return
      }

      @Suppress("DEPRECATION")
      window.navigationBarColor = color

      if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
        window.isNavigationBarContrastEnforced = false
      }

      if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
        // Null until a dialog window is added; the next focus change retries.
        val controller = window.insetsController ?: return
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

    /**
     * Paints the window of every modal dialog in the view tree.
     *
     * A `ReactModalHostView` stays in the React root as a zero-sized child while
     * its dialog is showing, and its `dialog` getter is public, so the dialog's
     * window is reachable without reflection or a patched React Native. The walk
     * is cheap and runs only on a theme change or a focus change, never per frame.
     */
    private fun applyToOpenDialogs(view: View?, color: Int, lightIcons: Boolean) {
      if (view == null) {
        return
      }

      if (view is ReactModalHostView) {
        applyToWindow(view.dialog?.window, color, lightIcons)
        return
      }

      if (view is ViewGroup) {
        for (index in 0 until view.childCount) {
          applyToOpenDialogs(view.getChildAt(index), color, lightIcons)
        }
      }
    }

    /** Paints the status bar's fill on one window. */
    private fun applyStatusBarBackground(window: Window?, color: Int) {
      if (window == null) {
        return
      }

      @Suppress("DEPRECATION")
      window.statusBarColor = color

      if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
        // Otherwise Android paints its own translucent scrim over the colour on
        // a bar that is not edge-to-edge.
        window.isStatusBarContrastEnforced = false
      }
    }

    /** Sets the status-bar fill and icon appearance on one window. */
    private fun applyStatusBarToWindow(window: Window?, lightContent: Boolean, color: Int) {
      if (window == null) {
        return
      }

      applyStatusBarBackground(window, color)

      // "Light content" is white icons, which is the OPPOSITE of Android's
      // `APPEARANCE_LIGHT_STATUS_BARS` flag (that flag asks for dark icons on a
      // light surface).
      val darkIcons = !lightContent

      if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
        // Null until a dialog window is added; the next focus change retries.
        val controller = window.insetsController ?: return
        val mask = WindowInsetsController.APPEARANCE_LIGHT_STATUS_BARS
        controller.setSystemBarsAppearance(if (darkIcons) mask else 0, mask)
      } else {
        // API 24-29: the light-status-bar flag is available from API 23.
        @Suppress("DEPRECATION")
        val flags = window.decorView.systemUiVisibility
        @Suppress("DEPRECATION")
        window.decorView.systemUiVisibility =
            if (darkIcons) {
              flags or View.SYSTEM_UI_FLAG_LIGHT_STATUS_BAR
            } else {
              flags and View.SYSTEM_UI_FLAG_LIGHT_STATUS_BAR.inv()
            }
      }
    }

    /** Applies the status-bar fill and icon appearance to every modal dialog's window. */
    private fun applyStatusBarToOpenDialogs(view: View?, lightContent: Boolean, color: Int) {
      if (view == null) {
        return
      }

      if (view is ReactModalHostView) {
        applyStatusBarToWindow(view.dialog?.window, lightContent, color)
        return
      }

      if (view is ViewGroup) {
        for (index in 0 until view.childCount) {
          applyStatusBarToOpenDialogs(view.getChildAt(index), lightContent, color)
        }
      }
    }
  }
}

