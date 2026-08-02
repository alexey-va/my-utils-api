package dev.myutils.api.infra.util

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch
import java.util.ArrayDeque
import java.util.concurrent.ConcurrentHashMap

/** Per-key FIFO processor: serial within one key, parallel across different keys. */
class PerKeySerialQueue<K, V>(
	private val scope: CoroutineScope,
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
				state.items.addLast(value)
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
					if (state.items.isEmpty()) {
						state.draining = false
						states.remove(key, state)
						return@drain
					}
					state.items.removeFirst()
				}
			processor(key, item)
		}
	}

	private class State<V>(
		val items: ArrayDeque<V> = ArrayDeque(),
		var draining: Boolean = false,
	)
}
