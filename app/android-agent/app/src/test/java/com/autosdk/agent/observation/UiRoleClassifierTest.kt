package com.autosdk.agent.observation

import org.junit.Assert.assertEquals
import org.junit.Test

class UiRoleClassifierTest {

    @Test
    fun `EditText with isEditable returns input`() {
        assertEquals("input", classifyUiRole("android.widget.EditText", isEditable = true, isClickable = false, isCheckable = false, isScrollable = false))
    }

    @Test
    fun `EditText class name without isEditable returns input`() {
        assertEquals("input", classifyUiRole("android.widget.EditText", isEditable = false, isClickable = false, isCheckable = false, isScrollable = false))
    }

    @Test
    fun `TextInputEditText returns input`() {
        assertEquals("input", classifyUiRole("com.google.android.material.textfield.TextInputEditText", isEditable = true, isClickable = false, isCheckable = false, isScrollable = false))
    }

    @Test
    fun `Button returns button`() {
        assertEquals("button", classifyUiRole("android.widget.Button", isEditable = false, isClickable = true, isCheckable = false, isScrollable = false))
    }

    @Test
    fun `ProgressBar returns loading`() {
        assertEquals("loading", classifyUiRole("android.widget.ProgressBar", isEditable = false, isClickable = false, isCheckable = false, isScrollable = false))
    }

    @Test
    fun `CircularProgressIndicator returns loading`() {
        assertEquals("loading", classifyUiRole("com.google.android.material.progressindicator.CircularProgressIndicator", isEditable = false, isClickable = false, isCheckable = false, isScrollable = false))
    }

    @Test
    fun `CheckBox returns checkbox`() {
        assertEquals("checkbox", classifyUiRole("android.widget.CheckBox", isEditable = false, isClickable = false, isCheckable = true, isScrollable = false))
    }

    @Test
    fun `Switch returns switch`() {
        assertEquals("switch", classifyUiRole("android.widget.Switch", isEditable = false, isClickable = false, isCheckable = true, isScrollable = false))
    }

    @Test
    fun `SwitchCompat returns switch`() {
        assertEquals("switch", classifyUiRole("androidx.appcompat.widget.SwitchCompat", isEditable = false, isClickable = false, isCheckable = true, isScrollable = false))
    }

    @Test
    fun `RadioButton returns radio`() {
        assertEquals("radio", classifyUiRole("android.widget.RadioButton", isEditable = false, isClickable = false, isCheckable = true, isScrollable = false))
    }

    @Test
    fun `null className returns container`() {
        assertEquals("container", classifyUiRole(null, isEditable = false, isClickable = false, isCheckable = false, isScrollable = false))
    }

    @Test
    fun `clickable TextView returns button`() {
        assertEquals("button", classifyUiRole("android.widget.TextView", isEditable = false, isClickable = true, isCheckable = false, isScrollable = false))
    }

    @Test
    fun `non-clickable TextView returns text`() {
        assertEquals("text", classifyUiRole("android.widget.TextView", isEditable = false, isClickable = false, isCheckable = false, isScrollable = false))
    }

    @Test
    fun `ImageView returns image`() {
        assertEquals("image", classifyUiRole("android.widget.ImageView", isEditable = false, isClickable = false, isCheckable = false, isScrollable = false))
    }

    @Test
    fun `RecyclerView returns list`() {
        assertEquals("list", classifyUiRole("androidx.recyclerview.widget.RecyclerView", isEditable = false, isClickable = false, isCheckable = false, isScrollable = false))
    }

    @Test
    fun `scrollable unknown view returns scroll_container`() {
        assertEquals("scroll_container", classifyUiRole("com.example.CustomScrollView", isEditable = false, isClickable = false, isCheckable = false, isScrollable = true))
    }

    @Test
    fun `unknown non-scrollable view returns container`() {
        assertEquals("container", classifyUiRole("com.example.CustomLayout", isEditable = false, isClickable = false, isCheckable = false, isScrollable = false))
    }
}
