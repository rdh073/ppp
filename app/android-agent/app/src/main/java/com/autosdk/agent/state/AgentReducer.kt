package com.autosdk.agent.state

data class AgentReduction(
    val state: AgentState,
    val effects: List<AgentEffect> = emptyList(),
)

object AgentReducer {
    fun reduce(
        state: AgentState,
        event: AgentEvent,
    ): AgentReduction =
        when (event) {
            AgentEvent.BootCompleted ->
                resetForFreshBoundary(state, "boot_completed")

            AgentEvent.PackageReplaced ->
                resetForFreshBoundary(state, "package_replaced")

            AgentEvent.ServiceConnected ->
                onServiceConnected(state)

            AgentEvent.ServiceInterrupted ->
                onServiceLost(state, AgentServicePhase.INTERRUPTED, "service_interrupted")

            AgentEvent.ServiceDestroyed ->
                onServiceLost(state, AgentServicePhase.STOPPED, "service_destroyed")

            AgentEvent.ConnectRequested ->
                onConnectRequested(state)

            AgentEvent.BackoffElapsed ->
                onBackoffElapsed(state)

            AgentEvent.SocketOpened ->
                onSocketOpened(state)

            is AgentEvent.SocketOpenFailed ->
                toBackoff(state, event.reason)

            is AgentEvent.HelloAccepted ->
                onRegistrationAccepted(
                    state = state,
                    expectedMode = AgentRegistrationMode.HELLO,
                    sessionId = event.sessionId,
                    acceptedAtEpochMs = event.atEpochMs,
                )

            is AgentEvent.ResumeAccepted ->
                onRegistrationAccepted(
                    state = state,
                    expectedMode = AgentRegistrationMode.RESUME,
                    sessionId = event.sessionId,
                    acceptedAtEpochMs = event.atEpochMs,
                )

            is AgentEvent.ResumeRejected ->
                onResumeRejected(state, event.reason)

            is AgentEvent.SocketClosed ->
                onSocketDropped(state, event.reason)

            is AgentEvent.SocketFailed ->
                onSocketDropped(state, event.reason)

            AgentEvent.HeartbeatTick ->
                if (state.transport == AgentTransportPhase.CONNECTED) {
                    AgentReduction(state, listOf(AgentEffect.SendHeartbeat))
                } else {
                    invalidTransition(state, event, "heartbeat tick requires connected transport")
                }

            is AgentEvent.HeartbeatAccepted ->
                if (state.transport == AgentTransportPhase.CONNECTED) {
                    AgentReduction(
                        state.copy(
                            lastHeartbeatAtEpochMs = event.atEpochMs,
                            lastError = null,
                        ),
                    )
                } else {
                    invalidTransition(state, event, "heartbeat ack requires connected transport")
                }

            is AgentEvent.ObserveStarted ->
                startExecution(
                    state = state,
                    requestId = event.requestId,
                    nextPhase = AgentExecutionPhase.HANDLING_OBSERVE,
                    operation = "observe",
                )

            is AgentEvent.ObserveFinished ->
                transitionToResponding(state, event.requestId)

            is AgentEvent.ObserveFailed ->
                transitionToResponding(state, event.requestId, event.reason)

            is AgentEvent.QueryStarted ->
                startExecution(
                    state = state,
                    requestId = event.requestId,
                    nextPhase = AgentExecutionPhase.HANDLING_QUERY,
                    operation = "query",
                )

            is AgentEvent.QueryFinished ->
                transitionToResponding(state, event.requestId)

            is AgentEvent.QueryFailed ->
                transitionToResponding(state, event.requestId, event.reason)

            is AgentEvent.ExecuteRequested ->
                startExecution(
                    state = state,
                    requestId = event.requestId,
                    nextPhase = AgentExecutionPhase.OBSERVING_BEFORE_ACTION,
                    operation = "execute",
                )

            is AgentEvent.SnapshotBeforeFailed ->
                transitionToResponding(state, event.requestId, event.reason)

            is AgentEvent.SnapshotBeforeBuilt ->
                transitionExecution(
                    state = state,
                    requestId = event.requestId,
                    nextPhase = AgentExecutionPhase.EXECUTING_ACTION,
                )

            is AgentEvent.ActionSucceeded ->
                transitionExecution(
                    state = state,
                    requestId = event.requestId,
                    nextPhase = AgentExecutionPhase.SETTLING,
                    lastError = null,
                )

            is AgentEvent.ActionFailed ->
                transitionToResponding(state, event.requestId, event.reason)

            is AgentEvent.SettleCompleted ->
                transitionExecution(
                    state = state,
                    requestId = event.requestId,
                    nextPhase = AgentExecutionPhase.OBSERVING_AFTER_ACTION,
                    extraEffects =
                        if (event.timedOut) {
                            listOf(AgentEffect.Log("settle timeout for ${event.requestId}"))
                        } else {
                            emptyList()
                        },
                )

            is AgentEvent.SnapshotAfterBuilt ->
                transitionToResponding(state, event.requestId)

            is AgentEvent.ResponseSent ->
                onResponseSent(state, event.requestId)
        }

    private fun resetForFreshBoundary(
        state: AgentState,
        reason: String,
    ): AgentReduction =
        AgentReduction(
            state =
                state.copy(
                    service = AgentServicePhase.AWAITING_SERVICE,
                    transport = AgentTransportPhase.DISCONNECTED,
                    execution = AgentExecutionPhase.IDLE,
                    registrationMode = null,
                    reconnectAttempt = 0,
                    lastHeartbeatAtEpochMs = null,
                    lastRegistrationAtEpochMs = null,
                    currentRequestId = null,
                    lastError = null,
                ),
            effects =
                buildList {
                    add(AgentEffect.StopHeartbeatTimer)
                    add(AgentEffect.DisconnectSocket)
                    add(AgentEffect.ClearInflightCommand)
                    add(AgentEffect.Log(reason))
                },
        )

    private fun onServiceConnected(state: AgentState): AgentReduction {
        if (!AgentTransitionTable.isServiceTransitionAllowed(state.service, AgentServicePhase.ACTIVE)) {
            return invalidTransition(state, AgentEvent.ServiceConnected, "service cannot move to active")
        }

        val shouldConnect = state.transport == AgentTransportPhase.DISCONNECTED
        val nextState =
            state.copy(
                service = AgentServicePhase.ACTIVE,
                transport =
                    if (shouldConnect) {
                        AgentTransportPhase.CONNECTING
                    } else {
                        state.transport
                    },
                lastError = null,
            )
        val effects =
            buildList {
                add(AgentEffect.Log("service_connected"))
                if (shouldConnect) {
                    add(AgentEffect.ConnectSocket)
                }
            }
        return AgentReduction(nextState, effects)
    }

    private fun onServiceLost(
        state: AgentState,
        nextServicePhase: AgentServicePhase,
        reason: String,
    ): AgentReduction {
        if (!AgentTransitionTable.isServiceTransitionAllowed(state.service, nextServicePhase)) {
            return invalidTransition(state, AgentEvent.ServiceInterrupted, "service loss transition is invalid")
        }

        val nextState =
            state.copy(
                service = nextServicePhase,
                transport = AgentTransportPhase.DISCONNECTED,
                execution = AgentExecutionPhase.IDLE,
                registrationMode = null,
                currentRequestId = null,
                lastError = reason,
            )
        val effects =
            buildList {
                if (state.sessionId != null && state.transport == AgentTransportPhase.CONNECTED) {
                    add(AgentEffect.SendDisconnect(state.sessionId))
                }
                add(AgentEffect.StopHeartbeatTimer)
                add(AgentEffect.DisconnectSocket)
                add(AgentEffect.ClearInflightCommand)
                add(AgentEffect.Log(reason))
            }
        return AgentReduction(nextState, effects)
    }

    private fun onConnectRequested(state: AgentState): AgentReduction {
        if (state.service != AgentServicePhase.ACTIVE) {
            return invalidTransition(state, AgentEvent.ConnectRequested, "connect requires active service")
        }
        if (!AgentTransitionTable.isTransportTransitionAllowed(state.transport, AgentTransportPhase.CONNECTING)) {
            return invalidTransition(state, AgentEvent.ConnectRequested, "transport cannot enter connecting")
        }

        return AgentReduction(
            state =
                state.copy(
                    transport = AgentTransportPhase.CONNECTING,
                    registrationMode = null,
                    lastError = null,
                ),
            effects = listOf(AgentEffect.ConnectSocket),
        )
    }

    private fun onBackoffElapsed(state: AgentState): AgentReduction {
        if (!AgentTransitionTable.isTransportTransitionAllowed(state.transport, AgentTransportPhase.CONNECTING)) {
            return invalidTransition(state, AgentEvent.BackoffElapsed, "backoff can only resume from backoff_wait")
        }

        return AgentReduction(
            state =
                state.copy(
                    transport = AgentTransportPhase.CONNECTING,
                    registrationMode = null,
                    lastError = null,
                ),
            effects = listOf(AgentEffect.ConnectSocket),
        )
    }

    private fun onSocketOpened(state: AgentState): AgentReduction {
        if (!AgentTransitionTable.isTransportTransitionAllowed(state.transport, AgentTransportPhase.REGISTERING)) {
            return invalidTransition(state, AgentEvent.SocketOpened, "socket open requires connecting state")
        }

        val nextMode =
            if (state.sessionId.isNullOrBlank()) {
                AgentRegistrationMode.HELLO
            } else {
                AgentRegistrationMode.RESUME
            }

        val effect =
            if (nextMode == AgentRegistrationMode.RESUME) {
                AgentEffect.SendResume(state.sessionId!!)
            } else {
                AgentEffect.SendHello
            }

        return AgentReduction(
            state =
                state.copy(
                    transport = AgentTransportPhase.REGISTERING,
                    registrationMode = nextMode,
                    lastError = null,
                ),
            effects =
                listOf(
                    effect,
                    AgentEffect.Log("socket_opened:${nextMode.name.lowercase()}"),
                ),
        )
    }

    private fun onRegistrationAccepted(
        state: AgentState,
        expectedMode: AgentRegistrationMode,
        sessionId: String,
        acceptedAtEpochMs: Long,
    ): AgentReduction {
        if (state.transport != AgentTransportPhase.REGISTERING || state.registrationMode != expectedMode) {
            return invalidTransition(
                state,
                if (expectedMode == AgentRegistrationMode.HELLO) {
                    AgentEvent.HelloAccepted(sessionId, acceptedAtEpochMs)
                } else {
                    AgentEvent.ResumeAccepted(sessionId, acceptedAtEpochMs)
                },
                "registration acknowledgement does not match current mode",
            )
        }
        if (!AgentTransitionTable.isTransportTransitionAllowed(state.transport, AgentTransportPhase.CONNECTED)) {
            return invalidTransition(
                state,
                if (expectedMode == AgentRegistrationMode.HELLO) {
                    AgentEvent.HelloAccepted(sessionId, acceptedAtEpochMs)
                } else {
                    AgentEvent.ResumeAccepted(sessionId, acceptedAtEpochMs)
                },
                "transport cannot enter connected",
            )
        }

        return AgentReduction(
            state =
                state.copy(
                    transport = AgentTransportPhase.CONNECTED,
                    registrationMode = null,
                    reconnectAttempt = 0,
                    sessionId = sessionId,
                    lastHeartbeatAtEpochMs = acceptedAtEpochMs,
                    lastRegistrationAtEpochMs = acceptedAtEpochMs,
                    lastError = null,
                ),
            effects =
                listOf(
                    AgentEffect.PersistSessionId(sessionId),
                    AgentEffect.StartHeartbeatTimer,
                ),
        )
    }

    private fun toBackoff(
        state: AgentState,
        reason: String,
    ): AgentReduction {
        if (!AgentTransitionTable.isTransportTransitionAllowed(state.transport, AgentTransportPhase.BACKOFF_WAIT)) {
            return invalidTransition(
                state,
                AgentEvent.SocketOpenFailed(reason),
                "transport cannot enter backoff_wait",
            )
        }

        val attempt = state.reconnectAttempt + 1
        return AgentReduction(
            state =
                state.copy(
                    transport = AgentTransportPhase.BACKOFF_WAIT,
                    registrationMode = null,
                    reconnectAttempt = attempt,
                    lastError = reason,
                ),
            effects =
                listOf(
                    AgentEffect.DisconnectSocket,
                    AgentEffect.ScheduleBackoff(attempt),
                    AgentEffect.Log("backoff_wait:$reason"),
                ),
        )
    }

    private fun onResumeRejected(
        state: AgentState,
        reason: String,
    ): AgentReduction {
        if (state.transport != AgentTransportPhase.REGISTERING || state.registrationMode != AgentRegistrationMode.RESUME) {
            return invalidTransition(state, AgentEvent.ResumeRejected(reason), "resume rejection requires registering resume mode")
        }

        val attempt = state.reconnectAttempt + 1
        return AgentReduction(
            state =
                state.copy(
                    transport = AgentTransportPhase.BACKOFF_WAIT,
                    registrationMode = null,
                    sessionId = null,
                    reconnectAttempt = attempt,
                    lastError = reason,
                ),
            effects =
                listOf(
                    AgentEffect.ClearSessionId,
                    AgentEffect.DisconnectSocket,
                    AgentEffect.ScheduleBackoff(attempt),
                    AgentEffect.Log("resume_rejected:$reason"),
                ),
        )
    }

    private fun onSocketDropped(
        state: AgentState,
        reason: String,
    ): AgentReduction {
        if (
            state.transport == AgentTransportPhase.BACKOFF_WAIT ||
            state.transport == AgentTransportPhase.DISCONNECTED
        ) {
            return AgentReduction(state)
        }

        if (state.service != AgentServicePhase.ACTIVE) {
            return AgentReduction(
                state =
                    state.copy(
                        transport = AgentTransportPhase.DISCONNECTED,
                        registrationMode = null,
                        execution = AgentExecutionPhase.IDLE,
                        currentRequestId = null,
                        lastError = reason,
                    ),
                effects = listOf(AgentEffect.StopHeartbeatTimer, AgentEffect.Log(reason)),
            )
        }

        if (!AgentTransitionTable.isTransportTransitionAllowed(state.transport, AgentTransportPhase.BACKOFF_WAIT)) {
            return invalidTransition(
                state,
                AgentEvent.SocketFailed(reason),
                "socket drop requires connecting/registering/connected transport",
            )
        }

        val attempt = state.reconnectAttempt + 1
        return AgentReduction(
            state =
                state.copy(
                    transport = AgentTransportPhase.BACKOFF_WAIT,
                    execution = AgentExecutionPhase.IDLE,
                    registrationMode = null,
                    reconnectAttempt = attempt,
                    currentRequestId = null,
                    lastError = reason,
                ),
            effects =
                buildList {
                    add(AgentEffect.StopHeartbeatTimer)
                    add(AgentEffect.DisconnectSocket)
                    add(AgentEffect.ClearInflightCommand)
                    add(AgentEffect.ScheduleBackoff(attempt))
                    if (state.currentRequestId != null) {
                        add(AgentEffect.Log("command_abandoned:${state.currentRequestId}:$reason"))
                    }
                    add(AgentEffect.Log("socket_dropped:$reason"))
                },
        )
    }

    private fun startExecution(
        state: AgentState,
        requestId: String,
        nextPhase: AgentExecutionPhase,
        operation: String,
    ): AgentReduction {
        if (state.service != AgentServicePhase.ACTIVE) {
            return invalidTransition(state, AgentEvent.ObserveStarted(requestId), "$operation requires active service")
        }
        if (!AgentTransitionTable.isExecutionTransitionAllowed(state.execution, nextPhase)) {
            return invalidTransition(state, AgentEvent.ObserveStarted(requestId), "execution cannot start $operation")
        }

        return AgentReduction(
            state =
                state.copy(
                    execution = nextPhase,
                    currentRequestId = requestId,
                    lastError = null,
                ),
        )
    }

    private fun transitionExecution(
        state: AgentState,
        requestId: String,
        nextPhase: AgentExecutionPhase,
        lastError: String? = state.lastError,
        extraEffects: List<AgentEffect> = emptyList(),
    ): AgentReduction {
        if (state.currentRequestId != requestId) {
            return invalidTransition(state, AgentEvent.ResponseSent(requestId), "request id does not match current execution")
        }
        if (!AgentTransitionTable.isExecutionTransitionAllowed(state.execution, nextPhase)) {
            return invalidTransition(state, AgentEvent.ResponseSent(requestId), "execution transition is invalid")
        }

        return AgentReduction(
            state =
                state.copy(
                    execution = nextPhase,
                    lastError = lastError,
                ),
            effects = extraEffects,
        )
    }

    private fun transitionToResponding(
        state: AgentState,
        requestId: String,
        lastError: String? = state.lastError,
    ): AgentReduction =
        transitionExecution(
            state = state,
            requestId = requestId,
            nextPhase = AgentExecutionPhase.RESPONDING,
            lastError = lastError,
        )

    private fun onResponseSent(
        state: AgentState,
        requestId: String,
    ): AgentReduction {
        if (state.currentRequestId != requestId) {
            return invalidTransition(state, AgentEvent.ResponseSent(requestId), "response sent does not match current request")
        }
        if (!AgentTransitionTable.isExecutionTransitionAllowed(state.execution, AgentExecutionPhase.IDLE)) {
            return invalidTransition(state, AgentEvent.ResponseSent(requestId), "response can only complete from responding or aborted execution")
        }

        return AgentReduction(
            state =
                state.copy(
                    execution = AgentExecutionPhase.IDLE,
                    currentRequestId = null,
                ),
        )
    }

    private fun invalidTransition(
        state: AgentState,
        event: AgentEvent,
        reason: String,
    ): AgentReduction =
        AgentReduction(
            state = state,
            effects =
                listOf(
                    AgentEffect.Log(
                        "invalid_transition:${event::class.simpleName}:$reason",
                    ),
                ),
        )
}
