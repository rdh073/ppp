package com.autosdk.agent.service

internal object AgentServerUrlResolver {
    fun resolve(defaultUrl: String): String {
        val sysProp =
            runCatching {
                Class.forName("android.os.SystemProperties")
                    .getMethod("get", String::class.java, String::class.java)
                    .invoke(null, "auto.agent.server_url", "") as String
            }.getOrElse { "" }
        return sysProp.takeIf { it.isNotBlank() } ?: defaultUrl
    }
}
