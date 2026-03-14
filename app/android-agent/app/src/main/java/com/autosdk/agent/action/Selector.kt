package com.autosdk.agent.action

/** Mirrors the SelectorKind enum in the contracts package. */
enum class SelectorKind {
    TEXT,
    RESOURCE_ID,
    TARGET_ID,
    CONTENT_DESC,
    BOUNDS,
    PACKAGE_NAME,
}

data class Selector(
    val kind: SelectorKind,
    val value: String,
)
