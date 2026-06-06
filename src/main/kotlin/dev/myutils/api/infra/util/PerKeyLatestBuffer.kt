package dev.myutils.api.infra.util

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch
import org.slf4j.Logger
import java.util.concurrent.ConcurrentHashMap

/**
 * Per-key serial processor that keeps only the latest value while busy.
 * Older values are dropped — useful for chat inboxes and offline update bursts.
 */
class PerKeyLatestBuffer<K, V>(
	private val scope: CoroutineScope,
	private val log: Logger,
	private val keyLabel: (K) -> String = { it.toString() },
	private val onDropped: ((K) -> Unit)? = null,
	private val processor: suspend (K, V) -> Unit,
) {
	private val states = ConcurrentHashMap<K, State<V>>()

	fun submit(
		key: K,
		value: V,
	) {
		val state = states.computeIfAbsent(key) { State() }
		val startDrain =
			synchronized(state) {
				val hadPending = state.latest != null
				state.latest = value
				if (hadPending) {
					onDropped?.invoke(key)
					log.debug("{} replaced pending item", keyLabel(key))
				}
				if (state.draining) {
					false
				} else {
					state.draining = true
					true
				}
			}
		if (startDrain) {
			scope.launch { drain(key, state) }
		}
	}

	private suspend fun drain(
		key: K,
		state: State<V>,
	) {
		while (true) {
			val item =
				synchronized(state) {
					val next = state.latest
					state.latest = null
					if (next == null) {
						state.draining = false
						states.remove(key, state)
						return@drain
					}
					next
				}
			processor(key, item)
		}
	}

	private class State<V>(
		var latest: V? = null,
		var draining: Boolean = false,
	)
}
