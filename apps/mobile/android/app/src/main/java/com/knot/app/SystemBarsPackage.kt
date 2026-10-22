package com.knot.app

import com.facebook.react.ReactPackage
import com.facebook.react.bridge.NativeModule
import com.facebook.react.bridge.ReactApplicationContext
import com.facebook.react.uimanager.ViewManager

/**
 * Registers [SystemBarsModule] with React Native.
 *
 * The module is app-local (there is one screen-level concern behind it and no
 * dependency to add), so it is added to the package list in `MainApplication`
 * rather than published as a library (KNOT-020j).
 */
class SystemBarsPackage : ReactPackage {
  override fun createNativeModules(reactContext: ReactApplicationContext): List<NativeModule> =
      listOf(SystemBarsModule(reactContext))

  override fun createViewManagers(
      reactContext: ReactApplicationContext
  ): List<ViewManager<*, *>> = emptyList()
}
