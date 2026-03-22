package com.autosdk.agent.service

import android.accessibilityservice.AccessibilityService
import android.graphics.Bitmap
import android.os.Build
import android.util.Base64
import android.util.Log
import android.view.Display
import java.io.ByteArrayOutputStream
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

internal class AccessibilityScreenshotCapture(
    private val service: AccessibilityService,
    private val logTag: String,
) {
    fun capture(): String? {
        if (Build.VERSION.SDK_INT < 30) return null

        val latch = CountDownLatch(1)
        var resultBase64: String? = null
        service.takeScreenshot(
            Display.DEFAULT_DISPLAY,
            service.mainExecutor,
            object : AccessibilityService.TakeScreenshotCallback {
                override fun onSuccess(screenshot: AccessibilityService.ScreenshotResult) {
                    try {
                        val hardwareBuffer = screenshot.hardwareBuffer ?: return
                        val hardwareBitmap = Bitmap.wrapHardwareBuffer(hardwareBuffer, screenshot.colorSpace)
                        hardwareBuffer.close()
                        hardwareBitmap ?: return
                        val softwareBitmap = hardwareBitmap.copy(Bitmap.Config.ARGB_8888, false)
                        hardwareBitmap.recycle()
                        val bytes = ByteArrayOutputStream()
                        softwareBitmap.compress(Bitmap.CompressFormat.PNG, 100, bytes)
                        softwareBitmap.recycle()
                        resultBase64 = Base64.encodeToString(bytes.toByteArray(), Base64.NO_WRAP)
                    } finally {
                        latch.countDown()
                    }
                }

                override fun onFailure(errorCode: Int) {
                    Log.w(logTag, "captureScreenshot: takeScreenshot failed errorCode=$errorCode")
                    latch.countDown()
                }
            },
        )
        if (!latch.await(5, TimeUnit.SECONDS)) {
            Log.w(logTag, "captureScreenshot: callback timed out")
        }
        return resultBase64
    }
}
