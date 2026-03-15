package com.autosdk.agent.observation

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ShouldIncludeNodeTest {

    @Test
    fun `visible node with area is included`() {
        assertTrue(shouldIncludeNode(visible = true, hasArea = true, hasId = false, hasText = false))
    }

    @Test
    fun `visible node without area is excluded when no id or text`() {
        assertFalse(shouldIncludeNode(visible = true, hasArea = false, hasId = false, hasText = false))
    }

    @Test
    fun `invisible node with area is excluded when no id or text`() {
        assertFalse(shouldIncludeNode(visible = false, hasArea = true, hasId = false, hasText = false))
    }

    @Test
    fun `node with id is included regardless of visibility`() {
        assertTrue(shouldIncludeNode(visible = false, hasArea = false, hasId = true, hasText = false))
    }

    @Test
    fun `node with text is included regardless of visibility`() {
        assertTrue(shouldIncludeNode(visible = false, hasArea = false, hasId = false, hasText = true))
    }

    @Test
    fun `node with id and text but invisible and zero area is included`() {
        assertTrue(shouldIncludeNode(visible = false, hasArea = false, hasId = true, hasText = true))
    }

    @Test
    fun `all false is excluded`() {
        assertFalse(shouldIncludeNode(visible = false, hasArea = false, hasId = false, hasText = false))
    }

    @Test
    fun `all true is included`() {
        assertTrue(shouldIncludeNode(visible = true, hasArea = true, hasId = true, hasText = true))
    }
}
