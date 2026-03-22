package com.autosdk.agent.service

import android.accessibilityservice.AccessibilityService
import android.os.Build
import android.provider.Settings
import android.util.Log
import com.autosdk.agent.action.ActionExecutor
import com.autosdk.agent.agent.AccessibilityAgentAutomationDriver
import com.autosdk.agent.agent.AgentCapabilities
import com.autosdk.agent.agent.AgentRuntime
import com.autosdk.agent.state.AgentEvent
import com.autosdk.agent.state.AgentLogger
import com.autosdk.agent.state.AgentRuntimeHooks
import com.autosdk.agent.state.AgentStateCoordinator
import com.autosdk.agent.state.AgentStateStore
import com.autosdk.agent.state.CoroutineBackoffScheduler
import com.autosdk.agent.state.CoroutineHeartbeatScheduler
import com.autosdk.agent.state.SharedPreferencesAgentStateStore
import com.autosdk.agent.transport.WebSocketAgentTransport
import com.autosdk.agent.transport.createSharedClient
import kotlinx.coroutines.CoroutineScope

internal data class ServiceRuntimeBundle(
    val deviceId: String,
    val stateStore: AgentStateStore,
    val lastOutboundEventSeqNo: Long,
    val transport: WebSocketAgentTransport,
    val coordinator: AgentStateCoordinator,
    val runtime: AgentRuntime,
)

internal class ServiceRuntimeBootstrap(
    private val service: AccessibilityService,
    private val scope: CoroutineScope,
    private val logTag: String,
    private val snapshotProvider: AccessibilitySnapshotProvider,
    private val eventAwaiter: AccessibilityEventAwaiter,
    private val awaitSettle: suspend () -> Unit,
    private val runtimeHooks: AgentRuntimeHooks,
    private val logger: AgentLogger,
) {
    fun bootstrap(): ServiceRuntimeBundle {
        val deviceId = Settings.Secure.getString(service.contentResolver, Settings.Secure.ANDROID_ID)
        val serverUrl = AgentServerUrlResolver.resolve(com.autosdk.agent.BuildConfig.SERVER_URL)
        val capabilityProvider = { AgentCapabilities.buildCapabilityList() }
        val deviceMetadataProvider = {
            mapOf(
                "manufacturer" to Build.MANUFACTURER,
                "model" to Build.MODEL,
                "device" to Build.DEVICE,
                "brand" to Build.BRAND,
                "product" to Build.PRODUCT,
                "androidVersion" to (Build.VERSION.RELEASE ?: ""),
                "sdkInt" to Build.VERSION.SDK_INT,
            )
        }
        val localStateStore = SharedPreferencesAgentStateStore.from(service.applicationContext)
        val persistedState = localStateStore.read()

        Log.i(logTag, "Preparing agent runtime for $serverUrl device=$deviceId")

        val ws =
            WebSocketAgentTransport(
                serverUrl = serverUrl,
                scope = scope,
            )

        lateinit var localCoordinator: AgentStateCoordinator
        val heartbeatScheduler =
            CoroutineHeartbeatScheduler(scope) {
                localCoordinator.dispatch(AgentEvent.HeartbeatTick)
            }
        val backoffScheduler =
            CoroutineBackoffScheduler(scope) {
                localCoordinator.dispatch(AgentEvent.BackoffElapsed)
            }
        localCoordinator =
            AgentStateCoordinator(
                scope = scope,
                deviceId = deviceId,
                store = localStateStore,
                transportDriver = ws,
                heartbeatScheduler = heartbeatScheduler,
                backoffScheduler = backoffScheduler,
                runtimeHooks = runtimeHooks,
                logger = logger,
                capabilitiesProvider = capabilityProvider,
                deviceMetadataProvider = deviceMetadataProvider,
            )

        val screenshotCapture = AccessibilityScreenshotCapture(service, logTag)
        val automationDriver = AccessibilityAgentAutomationDriver(ActionExecutor(service))
        val runtime =
            AgentRuntime(
                transport = ws,
                snapshotBuilder = { snapshotProvider.build(deviceId) },
                settle = awaitSettle,
                automationDriver = automationDriver,
                deviceId = deviceId,
                capabilities = capabilityProvider(),
                okHttpClient = createSharedClient(),
                onExecutionEvent = { event -> localCoordinator.dispatch(event) },
                screenshotCapture = { screenshotCapture.capture() },
                eventAwaiter = { kind, pkg, textContains, timeoutMs ->
                    eventAwaiter.await(kind, pkg, textContains, timeoutMs)
                },
            )

        return ServiceRuntimeBundle(
            deviceId = deviceId,
            stateStore = localStateStore,
            lastOutboundEventSeqNo = persistedState.lastOutboundEventSeqNo,
            transport = ws,
            coordinator = localCoordinator,
            runtime = runtime,
        )
    }
}
