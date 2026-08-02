package dev.myutils.api.agent

/** Fail-closed authorization for tools that change the diary, facts, or schedules. */
object AgentToolMutationPolicy {
	private val workoutNotation =
		Regex("""\d+(?:[.,]\d+)?[^.\n]{0,24}\d+\s*(?:[/xх*×])\s*\d+""", RegexOption.IGNORE_CASE)
	private val explicitWorkoutWrite =
		Regex("""(?:^|\s)(?:запиши(?:те)?|записать|залогируй(?:те)?|зафиксируй(?:те)?|добавь(?:те)?\s+(?:тренировку|в\s+дневник))(?:\s|$)""")
	private val explicitDelete =
		Regex("""(?:^|\s)(?:удали(?:те)?|удалить|сотри(?:те)?|стереть|убери(?:те)?|исправь(?:те)?\s+запись)(?:\s|$)""")
	private val explicitExerciseCreate =
		Regex("""(?:^|\s)(?:создай(?:те)?|создать|добавь(?:те)?)(?:\s+упражнение)?(?:\s|$)""")
	private val explicitExerciseRename = Regex("""(?:переименуй(?:те)?|переименовать|смени(?:те)?\s+название)""")
	private val bodyWeightNumber = Regex("""\d+(?:[.,]\d+)?(?:\s*(?:кг|kg|lb|lbs|фунт(?:а|ов)?))?""")
	private val bareBodyWeight =
		Regex("""^(?:вес\s*[:=]?\s*)?\d+(?:[.,]\d+)?\s*(?:кг|kg|lb|lbs|фунт(?:а|ов)?)[.!]?$""")
	private val bodyWeightStatement = Regex("""(?:взвесил(?:ся|ась)?|мой\s+вес|вешу).{0,20}\d""")
	private val naturalBodyWeightStatement =
		Regex(
			"""(?:^|\s)(?:вес(?:\s+(?:сегодня|вчера))?|(?:сегодня|вчера)\s+вес)\s*[:=—-]?\s*\d""",
		)
	private val explicitWeightWrite = Regex("""(?:запиши(?:те)?|записать|зафиксируй(?:те)?)\s+(?:мой\s+)?вес""")
	private val explicitRemember =
		Regex("""(?:^|\s)(?:запомни(?:те)?|учти(?:те)?|сохрани(?:те)?\s+(?:как\s+)?факт)(?=\s|[:—-]|$)""")
	private val explicitForget = Regex("""(?:^|\s)(?:забудь(?:те)?|удали(?:те)?\s+факт|больше\s+не\s+учитывай(?:те)?)(?:\s|$)""")
	private val explicitNotificationCreate =
		Regex("""(?:^|\s)(?:напомни(?:те)?|уведоми(?:те)?|поставь(?:те)?\s+напоминание|запланируй(?:те)?\s+(?:напоминание|уведомление))(?:\s|$)""")
	private val explicitNotificationCancel =
		Regex("""(?:отмени(?:те)?|удали(?:те)?|сними(?:те)?)\s+(?:это\s+)?(?:напоминание|уведомление)""")
	private val readOnlyQuestion =
		Regex("""^(?:что|ка(?:к|кой|кая|кие)|сколько|когда|почему|зачем|где|покажи|расскажи)\b""")

	fun denialReason(
		toolName: String,
		userMessage: String?,
	): String? {
		val normalizedTool = AgentToolCatalog.normalizeName(toolName)
		if (normalizedTool !in mutatingTools) {
			return null
		}
		val message = userMessage?.trim()?.lowercase().orEmpty()
		val question = message.contains('?') || readOnlyQuestion.containsMatchIn(message)
		val authorized =
			when (normalizedTool) {
				"create_exercise" -> !question && explicitExerciseCreate.containsMatchIn(message)
				"rename_exercise" -> explicitExerciseRename.containsMatchIn(message)
				"log_workout" ->
					explicitWorkoutWrite.containsMatchIn(message) ||
						(!question && workoutNotation.containsMatchIn(message))
				"delete_workout" -> explicitDelete.containsMatchIn(message)
				"log_body_weight" ->
					bodyWeightNumber.containsMatchIn(message) &&
						(
							explicitWeightWrite.containsMatchIn(message) ||
								(!question && bareBodyWeight.matches(message)) ||
								(!question && bodyWeightStatement.containsMatchIn(message)) ||
								(!question && naturalBodyWeightStatement.containsMatchIn(message))
						)
				"remember_fact" -> explicitRemember.containsMatchIn(message)
				"forget_fact" -> explicitForget.containsMatchIn(message)
				"manage_user_fact" ->
					explicitRemember.containsMatchIn(message) ||
						explicitForget.containsMatchIn(message)
				"send_notification", "schedule_notification" ->
					explicitNotificationCreate.containsMatchIn(message)
				"cancel_notification" -> explicitNotificationCancel.containsMatchIn(message)
				else -> false
			}
		return if (authorized) {
			null
		} else {
			"Текущее сообщение — только чтение: нет явной команды или данных для изменения."
		}
	}

	private val mutatingTools =
		setOf(
			"create_exercise",
			"rename_exercise",
			"log_workout",
			"delete_workout",
			"log_body_weight",
			"remember_fact",
			"forget_fact",
			"manage_user_fact",
			"send_notification",
			"schedule_notification",
			"cancel_notification",
		)
}
