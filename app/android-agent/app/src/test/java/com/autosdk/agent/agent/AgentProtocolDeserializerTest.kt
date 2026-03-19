package com.autosdk.agent.agent

import com.autosdk.agent.action.AutomationAction
import com.autosdk.agent.action.ScrollDirection
import com.autosdk.agent.action.Selector
import com.autosdk.agent.action.SelectorKind
import kotlinx.serialization.json.buildJsonArray
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class AgentProtocolDeserializerTest {

    // --- parseSelector ---

    @Test
    fun `parseSelector returns text selector`() {
        val obj = buildJsonObject { put("kind", "text"); put("value", "Sign In") }
        assertEquals(Selector(SelectorKind.TEXT, "Sign In"), AgentProtocolDeserializer.parseSelector(obj))
    }

    @Test
    fun `parseSelector returns resource_id selector`() {
        val obj = buildJsonObject { put("kind", "resource_id"); put("value", "com.example:id/btn") }
        assertEquals(Selector(SelectorKind.RESOURCE_ID, "com.example:id/btn"), AgentProtocolDeserializer.parseSelector(obj))
    }

    @Test
    fun `parseSelector returns semantic_key selector`() {
        val obj = buildJsonObject { put("kind", "semantic_key"); put("value", "button.sign_in") }
        assertEquals(Selector(SelectorKind.SEMANTIC_KEY, "button.sign_in"), AgentProtocolDeserializer.parseSelector(obj))
    }

    @Test
    fun `parseSelector returns target_id selector`() {
        val obj = buildJsonObject { put("kind", "target_id"); put("value", "snap_btn_0") }
        assertEquals(Selector(SelectorKind.TARGET_ID, "snap_btn_0"), AgentProtocolDeserializer.parseSelector(obj))
    }

    @Test
    fun `parseSelector returns coordinate selector`() {
        val obj = buildJsonObject { put("kind", "coordinate"); put("value", "540,960") }
        assertEquals(Selector(SelectorKind.COORDINATE, "540,960"), AgentProtocolDeserializer.parseSelector(obj))
    }

    @Test
    fun `parseSelector returns null for unknown kind`() {
        val obj = buildJsonObject { put("kind", "unknown"); put("value", "x") }
        assertNull(AgentProtocolDeserializer.parseSelector(obj))
    }

    @Test
    fun `parseSelector returns null when kind missing`() {
        val obj = buildJsonObject { put("value", "x") }
        assertNull(AgentProtocolDeserializer.parseSelector(obj))
    }

    @Test
    fun `parseSelector returns null when value missing`() {
        val obj = buildJsonObject { put("kind", "text") }
        assertNull(AgentProtocolDeserializer.parseSelector(obj))
    }

    // --- parseAction ---

    @Test
    fun `parseAction returns Click`() {
        val obj = buildJsonObject {
            put("kind", "click")
            put("target", buildJsonObject { put("kind", "text"); put("value", "OK") })
        }
        val action = AgentProtocolDeserializer.parseAction(obj)
        assertTrue(action is AutomationAction.Click)
        assertEquals(Selector(SelectorKind.TEXT, "OK"), (action as AutomationAction.Click).selector)
    }

    @Test
    fun `parseAction returns LongPress`() {
        val obj = buildJsonObject {
            put("kind", "long_press")
            put("target", buildJsonObject { put("kind", "resource_id"); put("value", "com.example:id/x") })
        }
        val action = AgentProtocolDeserializer.parseAction(obj)
        assertTrue(action is AutomationAction.LongPress)
    }

    @Test
    fun `parseAction returns InputText with value`() {
        val obj = buildJsonObject {
            put("kind", "input_text")
            put("target", buildJsonObject { put("kind", "text"); put("value", "Email") })
            put("inputText", "user@example.com")
        }
        val action = AgentProtocolDeserializer.parseAction(obj) as AutomationAction.InputText
        assertEquals("user@example.com", action.value)
    }

    @Test
    fun `parseAction returns InputText with empty string when inputText missing`() {
        val obj = buildJsonObject {
            put("kind", "input_text")
            put("target", buildJsonObject { put("kind", "text"); put("value", "Email") })
        }
        val action = AgentProtocolDeserializer.parseAction(obj) as AutomationAction.InputText
        assertEquals("", action.value)
    }

    @Test
    fun `parseAction returns Scroll forward by default`() {
        val obj = buildJsonObject {
            put("kind", "scroll")
            put("target", buildJsonObject { put("kind", "text"); put("value", "list") })
        }
        val action = AgentProtocolDeserializer.parseAction(obj) as AutomationAction.Scroll
        assertEquals(ScrollDirection.FORWARD, action.direction)
    }

    @Test
    fun `parseAction returns Scroll backward`() {
        val obj = buildJsonObject {
            put("kind", "scroll")
            put("direction", "backward")
        }
        val action = AgentProtocolDeserializer.parseAction(obj) as AutomationAction.Scroll
        assertEquals(ScrollDirection.BACKWARD, action.direction)
        assertNull(action.selector)
    }

    @Test
    fun `parseAction returns Home`() {
        val obj = buildJsonObject { put("kind", "home") }
        assertEquals(AutomationAction.Home, AgentProtocolDeserializer.parseAction(obj))
    }

    @Test
    fun `parseAction returns Back`() {
        val obj = buildJsonObject { put("kind", "back") }
        assertEquals(AutomationAction.Back, AgentProtocolDeserializer.parseAction(obj))
    }

    @Test
    fun `parseAction returns Wake`() {
        val obj = buildJsonObject { put("kind", "wake") }
        assertEquals(AutomationAction.Wake, AgentProtocolDeserializer.parseAction(obj))
    }

    @Test
    fun `parseAction returns Screenshot`() {
        val obj = buildJsonObject { put("kind", "screenshot") }
        assertEquals(AutomationAction.Screenshot, AgentProtocolDeserializer.parseAction(obj))
    }

    @Test
    fun `parseAction returns OpenApp`() {
        val obj = buildJsonObject {
            put("kind", "open_app")
            put("target", buildJsonObject { put("kind", "package_name"); put("value", "com.example.app") })
        }
        val action = AgentProtocolDeserializer.parseAction(obj) as AutomationAction.OpenApp
        assertEquals("com.example.app", action.packageName)
    }

    @Test
    fun `parseAction returns OpenIntent`() {
        val obj = buildJsonObject {
            put("kind", "open_intent")
            put("intentAction", "android.settings.ADD_ACCOUNT_SETTINGS")
            put("package", "com.android.settings")
        }
        val action = AgentProtocolDeserializer.parseAction(obj) as AutomationAction.OpenIntent
        assertEquals("android.settings.ADD_ACCOUNT_SETTINGS", action.intentAction)
        assertEquals("com.android.settings", action.packageName)
    }

    @Test
    fun `parseAction returns CloseApp`() {
        val obj = buildJsonObject { put("kind", "close_app") }
        assertEquals(AutomationAction.CloseApp, AgentProtocolDeserializer.parseAction(obj))
    }

    @Test
    fun `parseAction returns FillForm`() {
        val obj = buildJsonObject {
            put("kind", "fill_form")
            put("fields", buildJsonArray {
                add(buildJsonObject {
                    put("target", buildJsonObject { put("kind", "text"); put("value", "Email") })
                    put("value", "user@example.com")
                })
                add(buildJsonObject {
                    put("target", buildJsonObject { put("kind", "text"); put("value", "Password") })
                    put("value", "secret")
                })
            })
        }
        val action = AgentProtocolDeserializer.parseAction(obj) as AutomationAction.FillForm
        assertEquals(2, action.fields.size)
        assertEquals("user@example.com", action.fields[0].value)
        assertEquals("secret", action.fields[1].value)
    }

    @Test
    fun `parseAction returns null for FillForm with no valid fields`() {
        val obj = buildJsonObject {
            put("kind", "fill_form")
            put("fields", buildJsonArray { })
        }
        assertNull(AgentProtocolDeserializer.parseAction(obj))
    }

    @Test
    fun `parseAction returns null for unknown kind`() {
        val obj = buildJsonObject { put("kind", "fly") }
        assertNull(AgentProtocolDeserializer.parseAction(obj))
    }

    @Test
    fun `parseAction returns null when target selector is invalid`() {
        val obj = buildJsonObject {
            put("kind", "click")
            put("target", buildJsonObject { put("kind", "unknown"); put("value", "x") })
        }
        assertNull(AgentProtocolDeserializer.parseAction(obj))
    }
}
