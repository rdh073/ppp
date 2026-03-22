package com.autosdk.agent.architecture

import java.io.File
import org.junit.Assert.assertTrue
import org.junit.Test

class ArchitectureBoundaryTest {
    @Test
    fun `android agent stays free from business domain vocabulary`() {
        val sourceRoot = File(System.getProperty("user.dir"), "app/src/main/java/com/autosdk/agent")
        val forbiddenTerms = listOf(
            "campaign",
            "persona",
            "account creation",
            "account-creation",
            "account_manager",
            "account-manager",
        )

        val violations = sourceRoot
            .walkTopDown()
            .filter { it.isFile && it.extension == "kt" }
            .flatMap { file ->
                val content = file.readText().lowercase()
                forbiddenTerms
                    .filter { term -> content.contains(term) }
                    .map { term -> "${file.relativeTo(sourceRoot)} -> $term" }
            }
            .toList()

        assertTrue(
            "Forbidden business vocabulary leaked into android-agent:\n${violations.joinToString("\n")}",
            violations.isEmpty(),
        )
    }
}
