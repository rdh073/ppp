package com.autosdk.agent

import android.Manifest
import android.app.Activity
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import android.os.Bundle
import android.provider.Settings
import android.text.InputType
import android.widget.Button
import android.widget.EditText
import android.widget.LinearLayout
import android.widget.TextView
import android.widget.Toast
import com.autosdk.agent.service.AgentServerUrlResolver

/**
 * Minimal settings screen for configuring the agent's server URL without ADB.
 *
 * The saved URL takes effect after the accessibility service is restarted
 * (toggle it off and back on in Settings → Accessibility).
 */
class MainActivity : Activity() {

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        requestNotificationPermissionIfNeeded()

        val dp = resources.displayMetrics.density
        fun dp(v: Int) = (v * dp).toInt()

        val root = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(dp(24), dp(32), dp(24), dp(24))
        }

        root.addView(TextView(this).apply {
            text = "PPP Agent — Server Settings"
            textSize = 20f
        })

        root.addView(spacer(dp(24)))

        root.addView(TextView(this).apply {
            text = "Server URL"
            textSize = 14f
        })

        root.addView(spacer(dp(6)))

        val currentUrl = AgentServerUrlResolver.resolve(this, BuildConfig.SERVER_URL)
        val urlInput = EditText(this).apply {
            hint = "ws://192.168.x.x:3000/ws/agent"
            setText(currentUrl)
            inputType = InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_VARIATION_URI
            setSingleLine(true)
        }
        root.addView(urlInput)

        root.addView(spacer(dp(8)))

        root.addView(TextView(this).apply {
            text = "Default: ${BuildConfig.SERVER_URL}"
            textSize = 12f
            alpha = 0.6f
        })

        root.addView(spacer(dp(24)))

        root.addView(Button(this).apply {
            text = "Save"
            setOnClickListener {
                val url = urlInput.text.toString().trim()
                if (url.isEmpty()) {
                    Toast.makeText(this@MainActivity, "URL cannot be empty", Toast.LENGTH_SHORT).show()
                    return@setOnClickListener
                }
                if (!url.startsWith("ws://") && !url.startsWith("wss://")) {
                    Toast.makeText(this@MainActivity, "URL must start with ws:// or wss://", Toast.LENGTH_SHORT).show()
                    return@setOnClickListener
                }
                AgentServerUrlResolver.save(this@MainActivity, url)
                Toast.makeText(
                    this@MainActivity,
                    "Saved. Toggle the accessibility service to apply.",
                    Toast.LENGTH_LONG,
                ).show()
            }
        })

        root.addView(spacer(dp(12)))

        root.addView(Button(this).apply {
            text = "Open Accessibility Settings"
            setOnClickListener {
                startActivity(Intent(Settings.ACTION_ACCESSIBILITY_SETTINGS))
            }
        })

        root.addView(spacer(dp(24)))

        root.addView(TextView(this).apply {
            text = "After saving, open Accessibility Settings, disable the PPP Agent service, then re-enable it. The new URL will be used on the next connection."
            textSize = 13f
            alpha = 0.7f
        })

        root.addView(spacer(dp(24)))

        root.addView(Button(this).apply {
            text = "Reset to Default"
            alpha = 0.7f
            setOnClickListener {
                AgentServerUrlResolver.clear(this@MainActivity)
                urlInput.setText(BuildConfig.SERVER_URL)
                Toast.makeText(this@MainActivity, "Reset to default. Toggle service to apply.", Toast.LENGTH_SHORT).show()
            }
        })

        setContentView(root)
    }

    private fun requestNotificationPermissionIfNeeded() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            if (checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED) {
                requestPermissions(arrayOf(Manifest.permission.POST_NOTIFICATIONS), 0)
            }
        }
    }

    private fun spacer(height: Int) = android.view.View(this).apply {
        minimumHeight = height
    }
}
