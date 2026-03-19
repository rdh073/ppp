package com.autosdk.agent.agent

import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.addJsonObject
import kotlinx.serialization.json.buildJsonArray
import kotlinx.serialization.json.put

/**
 * Single source of truth for the agent's capability list and its JSON serialization.
 *
 * Both [AgentStateCoordinator] (via `capabilitiesProvider`) and [AgentRuntime]
 * (via `handleCapabilitiesGet`) delegate here so the capability set is defined
 * in exactly one place.
 */
internal object AgentCapabilities {

    /**
     * Returns the full capability list for this agent.
     * Each entry is a map with keys "name" (String), "available" (Boolean),
     * and optionally "reason" (String) when available is false.
     */
    fun buildCapabilityList(): List<Map<String, Any>> =
        listOf(
            mapOf("name" to "observe", "available" to true),
            mapOf("name" to "click", "available" to true),
            mapOf("name" to "long_press", "available" to true),
            mapOf("name" to "input_text", "available" to true),
            mapOf("name" to "delete_text", "available" to true),
            mapOf("name" to "scroll", "available" to true),
            mapOf("name" to "home", "available" to true),
            mapOf("name" to "back", "available" to true),
            mapOf("name" to "wake", "available" to true),
            mapOf("name" to "open_app", "available" to true),
            mapOf("name" to "open_intent", "available" to true),
            mapOf("name" to "close_app", "available" to true),
            mapOf("name" to "fill_form", "available" to true),
            mapOf(
                "name" to "screenshot",
                "available" to (android.os.Build.VERSION.SDK_INT >= 30),
                "reason" to if (android.os.Build.VERSION.SDK_INT < 30) "Requires API 30+" else "",
            ),
        )

    /**
     * Serializes [capabilities] to a JSON array suitable for the agent protocol wire format.
     */
    fun capabilitiesToJson(capabilities: List<Map<String, Any>>): JsonElement =
        buildJsonArray {
            capabilities.forEach { capability ->
                addJsonObject {
                    put("name", capability["name"] as String)
                    put("available", capability["available"] as Boolean)
                    (capability["reason"] as? String)
                        ?.takeIf { it.isNotBlank() }
                        ?.let { put("reason", it) }
                }
            }
        }
}
