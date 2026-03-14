package com.autosdk.agent.state

object AgentTransitionTable {
    private val serviceTransitions =
        mapOf(
            AgentServicePhase.AWAITING_SERVICE to
                setOf(AgentServicePhase.ACTIVE, AgentServicePhase.STOPPED),
            AgentServicePhase.ACTIVE to
                setOf(AgentServicePhase.INTERRUPTED, AgentServicePhase.STOPPED),
            AgentServicePhase.INTERRUPTED to
                setOf(AgentServicePhase.ACTIVE, AgentServicePhase.STOPPED),
            AgentServicePhase.STOPPED to emptySet(),
        )

    private val transportTransitions =
        mapOf(
            AgentTransportPhase.DISCONNECTED to
                setOf(AgentTransportPhase.CONNECTING),
            AgentTransportPhase.CONNECTING to
                setOf(
                    AgentTransportPhase.DISCONNECTED,
                    AgentTransportPhase.REGISTERING,
                    AgentTransportPhase.BACKOFF_WAIT,
                ),
            AgentTransportPhase.REGISTERING to
                setOf(
                    AgentTransportPhase.DISCONNECTED,
                    AgentTransportPhase.CONNECTED,
                    AgentTransportPhase.BACKOFF_WAIT,
                ),
            AgentTransportPhase.CONNECTED to
                setOf(
                    AgentTransportPhase.DISCONNECTED,
                    AgentTransportPhase.BACKOFF_WAIT,
                ),
            AgentTransportPhase.BACKOFF_WAIT to
                setOf(
                    AgentTransportPhase.DISCONNECTED,
                    AgentTransportPhase.CONNECTING,
                ),
        )

    private val executionTransitions =
        mapOf(
            AgentExecutionPhase.IDLE to
                setOf(
                    AgentExecutionPhase.HANDLING_OBSERVE,
                    AgentExecutionPhase.HANDLING_QUERY,
                    AgentExecutionPhase.OBSERVING_BEFORE_ACTION,
                ),
            AgentExecutionPhase.HANDLING_OBSERVE to
                setOf(
                    AgentExecutionPhase.RESPONDING,
                    AgentExecutionPhase.IDLE,
                ),
            AgentExecutionPhase.HANDLING_QUERY to
                setOf(
                    AgentExecutionPhase.RESPONDING,
                    AgentExecutionPhase.IDLE,
                ),
            AgentExecutionPhase.OBSERVING_BEFORE_ACTION to
                setOf(
                    AgentExecutionPhase.EXECUTING_ACTION,
                    AgentExecutionPhase.RESPONDING,
                    AgentExecutionPhase.IDLE,
                ),
            AgentExecutionPhase.EXECUTING_ACTION to
                setOf(
                    AgentExecutionPhase.SETTLING,
                    AgentExecutionPhase.RESPONDING,
                    AgentExecutionPhase.IDLE,
                ),
            AgentExecutionPhase.SETTLING to
                setOf(
                    AgentExecutionPhase.OBSERVING_AFTER_ACTION,
                    AgentExecutionPhase.IDLE,
                ),
            AgentExecutionPhase.OBSERVING_AFTER_ACTION to
                setOf(
                    AgentExecutionPhase.RESPONDING,
                    AgentExecutionPhase.IDLE,
                ),
            AgentExecutionPhase.RESPONDING to
                setOf(AgentExecutionPhase.IDLE),
        )

    fun isServiceTransitionAllowed(
        from: AgentServicePhase,
        to: AgentServicePhase,
    ): Boolean = serviceTransitions[from]?.contains(to) == true

    fun isTransportTransitionAllowed(
        from: AgentTransportPhase,
        to: AgentTransportPhase,
    ): Boolean = transportTransitions[from]?.contains(to) == true

    fun isExecutionTransitionAllowed(
        from: AgentExecutionPhase,
        to: AgentExecutionPhase,
    ): Boolean = executionTransitions[from]?.contains(to) == true
}
