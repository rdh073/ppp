package com.autosdk.agent.service

import android.util.Log
import com.autosdk.agent.state.AgentLogger

internal class ServiceAgentLogger(private val tag: String) : AgentLogger {
    override fun log(message: String) {
        Log.i(tag, message)
    }
}
