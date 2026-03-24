package com.autosdk.agent.service

import android.content.Context

private const val PREFS_NAME = "agent_state"
private const val KEY_SERVER_URL = "server_url"

internal object AgentServerUrlResolver {
    /**
     * Resolves the server WebSocket URL using the following priority chain:
     *  1. SharedPreferences — set via the in-app settings UI (MainActivity)
     *  2. System property `auto.agent.server_url` — set via `adb shell setprop`
     *  3. [defaultUrl] — baked-in BuildConfig value at compile time
     */
    fun resolve(context: Context, defaultUrl: String): String {
        // 1. SharedPreferences (in-app settings)
        val saved = context
            .getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
            .getString(KEY_SERVER_URL, null)
        if (!saved.isNullOrBlank()) return saved

        // 2. System property (adb shell setprop auto.agent.server_url <url>)
        val sysProp = runCatching {
            Class.forName("android.os.SystemProperties")
                .getMethod("get", String::class.java, String::class.java)
                .invoke(null, "auto.agent.server_url", "") as String
        }.getOrElse { "" }
        if (sysProp.isNotBlank()) return sysProp

        // 3. BuildConfig default
        return defaultUrl
    }

    /** Persists [url] to SharedPreferences. Effective after the next service restart. */
    fun save(context: Context, url: String) {
        context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
            .edit()
            .putString(KEY_SERVER_URL, url.trim())
            .apply()
    }

    /** Clears the persisted URL so the fallback chain takes over. */
    fun clear(context: Context) {
        context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
            .edit()
            .remove(KEY_SERVER_URL)
            .apply()
    }
}
