package com.autosdk.agent.observation

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class UiSemanticProjectionTest {
    @Test
    fun `projection builds form and button semantics from raw targets`() {
        val projection =
            buildSemanticProjection(
                rawTargets =
                    listOf(
                        target(
                            uiRole = "input",
                            resourceId = "com.example:id/email",
                            enabled = true,
                        ),
                        target(
                            uiRole = "input",
                            resourceId = "com.example:id/password",
                            enabled = true,
                            focused = true,
                        ),
                        target(
                            uiRole = "button",
                            resourceId = "com.example:id/submit",
                            text = "Submit",
                            enabled = true,
                            actionable = true,
                        ),
                    ),
                packageName = "com.example.app",
                activityName = "com.example.LoginActivity",
                screenState = "ready",
            )

        assertEquals("example.login", projection.semantic.baseScreenKey)
        assertEquals("example.login", projection.semantic.activeUiKey)
        assertEquals("form.primary.password", projection.semantic.focusedTargetKey)
        assertEquals(listOf("form.primary.email", "form.primary.password"), projection.semantic.forms.single().fieldKeys)
        assertEquals("form.primary.submit", projection.semantic.buttons.single().buttonKey)
        assertTrue(projection.semantic.uiReady)
    }

    @Test
    fun `dialog state uses overlay active ui key`() {
        val projection =
            buildSemanticProjection(
                rawTargets =
                    listOf(
                        target(uiRole = "text", text = "Permission required"),
                        target(uiRole = "button", text = "Allow", enabled = true, actionable = true),
                    ),
                packageName = "com.example.app",
                activityName = "com.example.MainActivity",
                screenState = "dialog",
            )

        assertEquals("example.main", projection.semantic.baseScreenKey)
        assertEquals("dialog.permission_required", projection.semantic.activeUiKey)
        assertEquals("dialog.permission_required", projection.semantic.overlayKey)
    }

    @Test
    fun `semantic digest changes when button enabled state changes`() {
        val disabled =
            buildSemanticProjection(
                rawTargets = listOf(target(uiRole = "button", text = "Continue", enabled = false, actionable = true)),
                packageName = "com.example.app",
                activityName = "com.example.MainActivity",
                screenState = "ready",
            )
        val enabled =
            buildSemanticProjection(
                rawTargets = listOf(target(uiRole = "button", text = "Continue", enabled = true, actionable = true)),
                packageName = "com.example.app",
                activityName = "com.example.MainActivity",
                screenState = "ready",
            )

        assertNotEquals(disabled.semantic.semanticDigest, enabled.semantic.semanticDigest)
    }

    private fun target(
        uiRole: String,
        text: String? = null,
        resourceId: String? = null,
        enabled: Boolean = true,
        actionable: Boolean = false,
        focused: Boolean = false,
    ): UiTarget =
        UiTarget(
            targetId = "target-${resourceId ?: text ?: uiRole}",
            role = uiRole,
            uiRole = uiRole,
            label = null,
            semanticKey = null,
            formKey = null,
            text = text,
            contentDesc = null,
            resourceId = resourceId,
            packageName = "com.example.app",
            bounds = intArrayOf(0, 0, 100, 40),
            actionable = actionable,
            enabled = enabled,
            checked = null,
            selected = false,
            scrollable = false,
            focused = focused,
            password = false,
        )
}
