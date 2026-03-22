package com.autosdk.agent.agent

import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class AgentCapabilitiesTest {

    @Test
    fun `buildCapabilityList contains all expected capabilities`() {
        val caps = AgentCapabilities.buildCapabilityList()
        val names = caps.map { it["name"] as String }.toSet()
        val expected = setOf(
            "observe", "click", "long_press", "input_text", "delete_text",
            "scroll", "home", "back", "wake", "open_app", "open_intent", "close_app",
            "fill_form", "script", "screenshot",
        )
        assertEquals(expected, names)
    }

    @Test
    fun `all capabilities except screenshot are available`() {
        val caps = AgentCapabilities.buildCapabilityList()
        caps.filter { it["name"] != "screenshot" }.forEach { cap ->
            assertTrue("${cap["name"]} should be available", cap["available"] == true)
        }
    }

    @Test
    fun `capabilitiesToJson serializes name and available fields`() {
        val caps = listOf(
            mapOf("name" to "observe", "available" to true),
            mapOf("name" to "click", "available" to false, "reason" to "test"),
        )
        val json = AgentCapabilities.capabilitiesToJson(caps)
        val arr = json.jsonArray
        assertEquals(2, arr.size)

        val first = arr[0].jsonObject
        assertEquals("observe", first["name"]!!.jsonPrimitive.content)
        assertTrue(first["available"]!!.jsonPrimitive.content.toBoolean())
        assertNull(first["reason"])

        val second = arr[1].jsonObject
        assertEquals("click", second["name"]!!.jsonPrimitive.content)
        assertFalse(second["available"]!!.jsonPrimitive.content.toBoolean())
        assertEquals("test", second["reason"]!!.jsonPrimitive.content)
    }

    @Test
    fun `capabilitiesToJson omits blank reason`() {
        val caps = listOf(mapOf("name" to "home", "available" to true, "reason" to ""))
        val json = AgentCapabilities.capabilitiesToJson(caps)
        val obj = json.jsonArray[0].jsonObject
        assertNull(obj["reason"])
    }

    @Test
    fun `capabilitiesToJson handles empty list`() {
        val json = AgentCapabilities.capabilitiesToJson(emptyList())
        assertTrue(json.jsonArray.isEmpty())
    }
}
