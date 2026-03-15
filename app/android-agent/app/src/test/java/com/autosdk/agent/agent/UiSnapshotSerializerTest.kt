package com.autosdk.agent.agent

import com.autosdk.agent.observation.UiButtonState
import com.autosdk.agent.observation.UiFormState
import com.autosdk.agent.observation.UiSemanticState
import com.autosdk.agent.observation.UiSnapshot
import com.autosdk.agent.observation.UiTarget
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class UiSnapshotSerializerTest {

    private val minimalSnapshot = UiSnapshot(
        snapshotId = "snap-1",
        deviceId = "dev-1",
        packageName = null,
        activityName = null,
        screenState = null,
        focusedTargetId = null,
        semantic = UiSemanticState(
            activeUiKey = "pkg.act",
            baseScreenKey = "pkg.act",
            overlayKey = null,
            uiReady = true,
            semanticDigest = "abc123",
            focusedTargetKey = null,
            forms = emptyList(),
            buttons = emptyList(),
        ),
        capturedAt = "2026-01-01T00:00:00Z",
        targets = emptyList(),
    )

    @Test
    fun `toJson includes snapshotId and deviceId`() {
        val obj = UiSnapshotSerializer.toJson(minimalSnapshot).jsonObject
        assertEquals("snap-1", obj["snapshotId"]!!.jsonPrimitive.content)
        assertEquals("dev-1", obj["deviceId"]!!.jsonPrimitive.content)
    }

    @Test
    fun `toJson omits null optional fields`() {
        val obj = UiSnapshotSerializer.toJson(minimalSnapshot).jsonObject
        assertNull(obj["packageName"])
        assertNull(obj["activityName"])
        assertNull(obj["screenState"])
        assertNull(obj["focusedTargetId"])
    }

    @Test
    fun `toJson includes optional fields when present`() {
        val snapshot = minimalSnapshot.copy(
            packageName = "com.example",
            activityName = "MainActivity",
            screenState = "ready",
            focusedTargetId = "target-1",
        )
        val obj = UiSnapshotSerializer.toJson(snapshot).jsonObject
        assertEquals("com.example", obj["packageName"]!!.jsonPrimitive.content)
        assertEquals("MainActivity", obj["activityName"]!!.jsonPrimitive.content)
        assertEquals("ready", obj["screenState"]!!.jsonPrimitive.content)
        assertEquals("target-1", obj["focusedTargetId"]!!.jsonPrimitive.content)
    }

    @Test
    fun `toJson serializes semantic block`() {
        val snapshot = minimalSnapshot.copy(
            semantic = minimalSnapshot.semantic.copy(
                activeUiKey = "com.example.main",
                baseScreenKey = "com.example.main",
                overlayKey = "dialog.confirm",
                uiReady = false,
                semanticDigest = "digest-xyz",
                focusedTargetKey = "form.primary.email",
                forms = listOf(UiFormState("form.primary", listOf("form.primary.email"), "form.primary.email", true)),
                buttons = listOf(UiButtonState("button.sign_in", enabled = true, visible = true, primary = true)),
            ),
        )
        val semantic = UiSnapshotSerializer.toJson(snapshot).jsonObject["semantic"]!!.jsonObject
        assertEquals("com.example.main", semantic["activeUiKey"]!!.jsonPrimitive.content)
        assertEquals("dialog.confirm", semantic["overlayKey"]!!.jsonPrimitive.content)
        assertFalse(semantic["uiReady"]!!.jsonPrimitive.content.toBoolean())
        assertEquals("digest-xyz", semantic["semanticDigest"]!!.jsonPrimitive.content)
        assertEquals("form.primary.email", semantic["focusedTargetKey"]!!.jsonPrimitive.content)
        assertEquals(1, semantic["forms"]!!.jsonArray.size)
        assertEquals(1, semantic["buttons"]!!.jsonArray.size)
    }

    @Test
    fun `toJson semantic omits overlayKey when null`() {
        val semantic = UiSnapshotSerializer.toJson(minimalSnapshot).jsonObject["semantic"]!!.jsonObject
        assertNull(semantic["overlayKey"])
        assertNull(semantic["focusedTargetKey"])
    }

    @Test
    fun `toJson serializes targets array`() {
        val snapshot = minimalSnapshot.copy(
            targets = listOf(
                UiTarget(
                    targetId = "t-1",
                    role = "Button",
                    uiRole = "button",
                    label = null,
                    semanticKey = "button.submit",
                    formKey = null,
                    text = "Submit",
                    contentDesc = null,
                    resourceId = "com.example:id/submit",
                    packageName = "com.example",
                    bounds = intArrayOf(0, 0, 100, 50),
                    actionable = true,
                    enabled = true,
                    checked = null,
                    selected = false,
                    scrollable = false,
                    focused = true,
                    password = false,
                ),
            ),
        )
        val targets = UiSnapshotSerializer.toJson(snapshot).jsonObject["targets"]!!.jsonArray
        assertEquals(1, targets.size)
        val t = targets[0].jsonObject
        assertEquals("t-1", t["targetId"]!!.jsonPrimitive.content)
        assertEquals("button", t["uiRole"]!!.jsonPrimitive.content)
        assertEquals("Submit", t["text"]!!.jsonPrimitive.content)
        assertTrue(t["focused"]!!.jsonPrimitive.content.toBoolean())
        assertNull(t["checked"])
    }

    @Test
    fun `toJson empty targets array is present`() {
        val targets = UiSnapshotSerializer.toJson(minimalSnapshot).jsonObject["targets"]!!.jsonArray
        assertTrue(targets.isEmpty())
    }

    // --- targetToJson ---

    @Test
    fun `targetToJson serializes bounds as array`() {
        val target = UiTarget(
            targetId = "t-2",
            role = null,
            uiRole = "text",
            label = null,
            semanticKey = null,
            formKey = null,
            text = "Hello",
            contentDesc = null,
            resourceId = null,
            packageName = null,
            bounds = intArrayOf(10, 20, 110, 70),
            actionable = false,
            enabled = true,
            checked = null,
            selected = false,
            scrollable = false,
            focused = false,
            password = false,
        )
        val obj = UiSnapshotSerializer.targetToJson(target).jsonObject
        val bounds = obj["bounds"]!!.jsonArray
        assertEquals(4, bounds.size)
        assertEquals(10, bounds[0].jsonPrimitive.content.toInt())
        assertEquals(20, bounds[1].jsonPrimitive.content.toInt())
        assertEquals(110, bounds[2].jsonPrimitive.content.toInt())
        assertEquals(70, bounds[3].jsonPrimitive.content.toInt())
    }

    @Test
    fun `targetToJson includes checked when non-null`() {
        val target = UiTarget(
            targetId = "t-3", role = null, uiRole = "checkbox", label = null,
            semanticKey = null, formKey = null, text = null, contentDesc = null,
            resourceId = null, packageName = null,
            bounds = intArrayOf(0, 0, 50, 50),
            actionable = true, enabled = true, checked = true,
            selected = false, scrollable = false, focused = false, password = false,
        )
        val obj = UiSnapshotSerializer.targetToJson(target).jsonObject
        assertTrue(obj["checked"]!!.jsonPrimitive.content.toBoolean())
    }

    @Test
    fun `targetToJson omits null optional fields`() {
        val target = UiTarget(
            targetId = "t-4", role = null, uiRole = "text", label = null,
            semanticKey = null, formKey = null, text = null, contentDesc = null,
            resourceId = null, packageName = null,
            bounds = intArrayOf(0, 0, 10, 10),
            actionable = false, enabled = true, checked = null,
            selected = false, scrollable = false, focused = false, password = false,
        )
        val obj = UiSnapshotSerializer.targetToJson(target).jsonObject
        assertNull(obj["role"])
        assertNull(obj["label"])
        assertNull(obj["semanticKey"])
        assertNull(obj["formKey"])
        assertNull(obj["text"])
        assertNull(obj["resourceId"])
        assertNull(obj["packageName"])
        assertNull(obj["checked"])
    }
}
