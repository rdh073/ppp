package com.autosdk.agent.agent.script

import org.mozilla.javascript.Context
import org.mozilla.javascript.ContextFactory
import org.mozilla.javascript.EcmaError
import org.mozilla.javascript.NativeObject
import org.mozilla.javascript.RhinoException
import org.mozilla.javascript.ScriptableObject

/**
 * Executes a JavaScript string in a sandboxed Rhino interpreter.
 *
 * The script runs in interpreted mode (optimizationLevel = -1), which is mandatory
 * on Android because the JVM bytecode generator is not compatible with DEX/ART.
 *
 * Timeout is enforced via a custom [ContextFactory] that overrides
 * [ContextFactory.observeInstructionCount]: every 10 000 instructions the factory
 * checks the wall-clock deadline and throws [ScriptTimeoutException] if it has passed.
 *
 * The script receives a `params` object populated from [execute]'s params map.
 * Its return value is collected as [ScriptResult.output].
 */
class JsRuntime(private val bridge: JsAutomationBridge) {

    data class ScriptResult(
        val output: Map<String, Any?>,
        val logs: List<String>,
        val durationMs: Long,
    )

    private class ScriptTimeoutException(message: String) : RuntimeException(message)

    /**
     * Executes [source] with [params] injected as the `params` variable.
     * Runs synchronously — call from a background thread (e.g. [kotlinx.coroutines.Dispatchers.IO]).
     *
     * Returns [Result.success] with [ScriptResult] on success, or
     * [Result.failure] with an exception whose message describes the error.
     */
    fun execute(source: String, params: Map<String, String>, timeoutMs: Long): Result<ScriptResult> {
        val start = System.currentTimeMillis()
        val deadline = start + timeoutMs

        val factory = object : ContextFactory() {
            override fun observeInstructionCount(cx: Context, instructionCount: Int) {
                if (System.currentTimeMillis() > deadline) {
                    throw ScriptTimeoutException("Script timed out after ${timeoutMs}ms")
                }
            }
        }

        val cx = factory.enterContext()
        return try {
            cx.optimizationLevel = -1            // MANDATORY: no JVM bytecode on Android/DEX
            cx.languageVersion = Context.VERSION_ES6
            cx.setInstructionObserverThreshold(10_000)

            val scope = cx.initStandardObjects()
            bridge.register(cx, scope)

            // Inject params as a JS object
            val paramsObj = cx.newObject(scope) as NativeObject
            for ((k, v) in params) {
                ScriptableObject.putProperty(paramsObj, k, v)
            }
            ScriptableObject.putProperty(scope, "params", paramsObj)

            // Wrap source so `return` at the top level works via an IIFE
            val wrapped = "(function() { $source })()"

            val rawResult = try {
                cx.evaluateString(scope, wrapped, "<script>", 1, null)
            } catch (e: ScriptTimeoutException) {
                return Result.failure(Exception(e.message))
            } catch (e: EcmaError) {
                return Result.failure(Exception(e.errorMessage ?: e.message ?: "Script error"))
            } catch (e: RhinoException) {
                return Result.failure(Exception(e.message ?: "Script error"))
            }

            val output = nativeObjectToMap(rawResult)
            val durationMs = System.currentTimeMillis() - start
            Result.success(ScriptResult(output = output, logs = bridge.logs.toList(), durationMs = durationMs))
        } catch (e: ScriptTimeoutException) {
            Result.failure(Exception(e.message))
        } catch (e: Exception) {
            Result.failure(e)
        } finally {
            Context.exit()
        }
    }

    // ---- helpers ----

    private fun nativeObjectToMap(value: Any?): Map<String, Any?> {
        if (value !is NativeObject) return emptyMap()
        return value.ids.filterIsInstance<String>().associate { key ->
            val v = value.get(key, value)
            key to when (v) {
                is Boolean -> v
                is Number -> v
                is String -> v
                Context.getUndefinedValue() -> null
                is NativeObject -> nativeObjectToMap(v)
                else -> v?.toString()
            }
        }
    }
}
