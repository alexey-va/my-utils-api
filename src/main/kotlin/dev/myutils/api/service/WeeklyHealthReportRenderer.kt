package dev.myutils.api.service

import org.springframework.stereotype.Service
import java.awt.BasicStroke
import java.awt.Color
import java.awt.Font
import java.awt.GradientPaint
import java.awt.Graphics2D
import java.awt.RenderingHints
import java.awt.geom.Ellipse2D
import java.awt.geom.Path2D
import java.awt.geom.RoundRectangle2D
import java.awt.image.BufferedImage
import java.io.ByteArrayOutputStream
import java.time.LocalDate
import javax.imageio.ImageIO
import kotlin.math.ceil
import kotlin.math.floor
import kotlin.math.max
import kotlin.math.roundToInt

/** Clean Telegram-ready weekly snapshots of the public Workout health charts. */
@Service
class WeeklyHealthReportRenderer {
	data class StepPoint(
		val date: LocalDate,
		val steps: Int,
	)

	data class WeightPoint(
		val date: LocalDate,
		val weightKg: Double,
	)

	fun renderSteps(
		points: List<StepPoint>,
		from: LocalDate,
		to: LocalDate,
	): ByteArray {
		val sorted = points.filter { it.date in from..to }.sortedBy { it.date }
		val chartFrom = sorted.firstOrNull()?.date ?: from
		val chartTo = sorted.lastOrNull()?.date ?: to
		val periodDays = (chartTo.toEpochDay() - chartFrom.toEpochDay() + 1).coerceAtLeast(1)
		val image = canvas()
		val g = image.cleanGraphics()
		drawHeader(
			g = g,
			title = "Шаги",
			subtitle = "${shortDate(chartFrom)} — ${shortDate(chartTo)}  ·  данные за ${sorted.size} из $periodDays дней",
			hero = sorted.lastOrNull()?.steps?.let(::formatInteger) ?: "—",
			heroLabel = sorted.lastOrNull()?.date?.let(::shortDate) ?: "нет данных",
			accent = Palette.STEPS,
		)
		if (sorted.isEmpty()) {
			drawEmpty(g, "Шаги за этот период ещё не загружены")
		} else {
			drawStepsHistogram(g, sorted, chartFrom, chartTo)
			drawStats(g, stepsStats(sorted))
		}
		g.dispose()
		return encode(image)
	}

	fun renderWeight(
		points: List<WeightPoint>,
		from: LocalDate,
		to: LocalDate,
	): ByteArray {
		val sorted = points.sortedBy { it.date }
		val image = canvas()
		val g = image.cleanGraphics()
		drawHeader(
			g = g,
			title = "Вес",
			subtitle = periodSubtitle(from, to, sorted.size, "замера"),
			hero = sorted.lastOrNull()?.let { "${formatDecimal(it.weightKg)} кг" } ?: "—",
			heroLabel = sorted.lastOrNull()?.date?.let(::shortDate) ?: "нет данных",
			accent = Palette.WEIGHT_LINE,
		)
		if (sorted.isEmpty()) {
			drawEmpty(g, "Вес за этот период ещё не записывали")
		} else {
			drawWeightTrend(g, sorted)
			drawStats(g, weightStats(sorted))
		}
		g.dispose()
		return encode(image)
	}

	private fun drawHeader(
		g: Graphics2D,
		title: String,
		subtitle: String,
		hero: String,
		heroLabel: String,
		accent: Color,
	) {
		g.font = font(Font.BOLD, 34f)
		g.color = Palette.TEXT
		g.drawString(title, CONTENT_LEFT, 54)

		g.font = font(Font.PLAIN, 15f)
		g.color = Palette.TEXT_MUTED
		g.drawString(subtitle, CONTENT_LEFT, 82)

		g.font = font(Font.BOLD, 32f)
		g.color = accent
		drawRight(g, hero, CONTENT_RIGHT, 52)
		g.font = font(Font.PLAIN, 13f)
		g.color = Palette.TEXT_MUTED
		drawRight(g, heroLabel, CONTENT_RIGHT, 78)
	}

	private fun drawStepsHistogram(
		g: Graphics2D,
		points: List<StepPoint>,
		from: LocalDate,
		to: LocalDate,
	) {
		val maxValue = max(points.maxOf { it.steps }, STEP_TARGET)
		val axisMax = max(5_000, ceil(maxValue / 5_000.0).toInt() * 5_000)
		drawGrid(
			g = g,
			min = 0.0,
			max = axisMax.toDouble(),
			formatter = { formatInteger(it.roundToInt()) },
		)

		val targetY = yFor(STEP_TARGET.toDouble(), 0.0, axisMax.toDouble())
		g.color = Palette.TARGET_LINE
		g.stroke = dashedStroke(1.5f, 7f, 7f)
		g.drawLine(PLOT_LEFT, targetY, PLOT_RIGHT, targetY)
		g.font = font(Font.BOLD, 12f)
		g.color = Palette.TARGET
		g.drawString("цель", PLOT_RIGHT + 10, targetY + 4)

		val dayCount = (to.toEpochDay() - from.toEpochDay() + 1).coerceAtLeast(1)
		val slot = PLOT_WIDTH.toDouble() / dayCount
		val width = (slot * 0.66).coerceIn(3.0, 11.0)
		val latestDate = points.last().date
		points
			.filter { it.date in from..to }
			.forEach { point ->
				val dayIndex = point.date.toEpochDay() - from.toEpochDay()
				val x = PLOT_LEFT + dayIndex * slot + (slot - width) / 2.0
				val top = yFor(point.steps.toDouble(), 0.0, axisMax.toDouble()).toDouble()
				val height = (PLOT_BOTTOM - top).coerceAtLeast(2.0)
				g.color =
					when {
						point.date == latestDate -> Palette.STEPS_LATEST
						point.steps >= STEP_TARGET -> Palette.STEPS_HIGH
						else -> Palette.STEPS_MUTED
					}
				g.fill(
					RoundRectangle2D.Double(
						x,
						top,
						width,
						height,
						minOf(5.0, width),
						minOf(5.0, width),
					),
				)
			}

		drawCalendarDateLabels(g, from, to)
	}

	private fun drawWeightTrend(
		g: Graphics2D,
		points: List<WeightPoint>,
	) {
		val rawMin = points.minOf { it.weightKg }
		val rawMax = points.maxOf { it.weightKg }
		val padding = ((rawMax - rawMin) * 0.45).coerceAtLeast(0.45)
		val axisMin = floor((rawMin - padding) * 2.0) / 2.0
		val axisMax = ceil((rawMax + padding) * 2.0) / 2.0
		drawGrid(
			g = g,
			min = axisMin,
			max = axisMax,
			formatter = ::formatDecimal,
		)

		val firstDate = points.first().date
		val lastDate = points.last().date
		val dayRange = (lastDate.toEpochDay() - firstDate.toEpochDay()).coerceAtLeast(1)
		val coordinates =
			points.map { point ->
				val fraction = (point.date.toEpochDay() - firstDate.toEpochDay()).toDouble() / dayRange
				ChartPoint(
					x = PLOT_LEFT + fraction * PLOT_WIDTH,
					y = yFor(point.weightKg, axisMin, axisMax).toDouble(),
				)
			}

		val average = points.map { it.weightKg }.average()
		val averageY = yFor(average, axisMin, axisMax)
		g.color = Palette.AVERAGE_LINE
		g.stroke = dashedStroke(1.25f, 5f, 7f)
		g.drawLine(PLOT_LEFT, averageY, PLOT_RIGHT, averageY)
		drawPill(
			g,
			"ср. ${formatDecimal(average)}",
			PLOT_RIGHT - 82,
			averageY - 27,
			Palette.TEXT_MUTED,
			Palette.PILL_DARK,
		)

		val line = linePath(coordinates)
		val fill =
			Path2D.Double(line).apply {
				lineTo(coordinates.last().x, PLOT_BOTTOM.toDouble())
				lineTo(coordinates.first().x, PLOT_BOTTOM.toDouble())
				closePath()
			}
		g.paint =
			GradientPaint(
				0f,
				PLOT_TOP.toFloat(),
				Palette.WEIGHT_FILL_TOP,
				0f,
				PLOT_BOTTOM.toFloat(),
				Palette.WEIGHT_FILL_BOTTOM,
			)
		g.fill(fill)

		g.color = Palette.WEIGHT_LINE
		g.stroke = BasicStroke(3f, BasicStroke.CAP_ROUND, BasicStroke.JOIN_ROUND)
		g.draw(line)

		coordinates.forEachIndexed { index, point ->
			val latest = index == coordinates.lastIndex
			val radius = if (latest) 7.0 else 4.5
			g.color = Palette.BG
			g.fill(Ellipse2D.Double(point.x - radius, point.y - radius, radius * 2, radius * 2))
			g.color = if (latest) Palette.LATEST else Palette.WEIGHT_LINE
			g.stroke = BasicStroke(if (latest) 3f else 2f)
			g.draw(Ellipse2D.Double(point.x - radius, point.y - radius, radius * 2, radius * 2))
		}

		drawDateLabels(
			g,
			points.map { it.date },
			coordinates.map { it.x },
		)
	}

	private fun drawGrid(
		g: Graphics2D,
		min: Double,
		max: Double,
		formatter: (Double) -> String,
	) {
		g.font = font(Font.PLAIN, 12f)
		for (index in 0..GRID_LINES) {
			val fraction = index.toDouble() / GRID_LINES
			val y = PLOT_BOTTOM - (PLOT_HEIGHT * fraction).roundToInt()
			val value = min + (max - min) * fraction
			g.color = Palette.GRID
			g.stroke = BasicStroke(1f)
			g.drawLine(PLOT_LEFT, y, PLOT_RIGHT, y)
			g.color = Palette.TEXT_DIM
			drawRight(g, formatter(value), PLOT_LEFT - 12, y + 4)
		}
	}

	private fun drawDateLabels(
		g: Graphics2D,
		dates: List<LocalDate>,
		xPositions: List<Double>,
	) {
		val indices =
			listOf(0, dates.lastIndex / 4, dates.lastIndex / 2, dates.lastIndex * 3 / 4, dates.lastIndex)
				.distinct()
		g.font = font(Font.PLAIN, 12f)
		g.color = Palette.TEXT_DIM
		indices.forEach { index ->
			val label = shortDate(dates[index])
			val width = g.fontMetrics.stringWidth(label)
			val centered = (xPositions[index] - width / 2.0).roundToInt()
			val x = centered.coerceIn(PLOT_LEFT, PLOT_RIGHT - width)
			g.drawString(label, x, PLOT_BOTTOM + 28)
		}
	}

	private fun drawCalendarDateLabels(
		g: Graphics2D,
		from: LocalDate,
		to: LocalDate,
	) {
		val dayRange = (to.toEpochDay() - from.toEpochDay()).coerceAtLeast(1)
		val dates =
			(0..4).map { index ->
				from.plusDays((dayRange * index / 4.0).roundToInt().toLong())
			}
		val xPositions =
			(0..4).map { index ->
				PLOT_LEFT + PLOT_WIDTH * index / 4.0
			}
		drawDateLabels(g, dates, xPositions)
	}

	private fun drawStats(
		g: Graphics2D,
		stats: List<Stat>,
	) {
		g.color = Palette.SEPARATOR
		g.stroke = BasicStroke(1f)
		g.drawLine(CONTENT_LEFT, STATS_TOP, CONTENT_RIGHT, STATS_TOP)

		val cellWidth = (CONTENT_RIGHT - CONTENT_LEFT) / stats.size
		stats.forEachIndexed { index, stat ->
			val x = CONTENT_LEFT + index * cellWidth
			g.font = font(Font.PLAIN, 12f)
			g.color = Palette.TEXT_DIM
			g.drawString(stat.label.uppercase(), x, STATS_TOP + 26)
			g.font = font(Font.BOLD, 19f)
			g.color = stat.color
			g.drawString(stat.value, x, STATS_TOP + 52)
		}
	}

	private fun drawEmpty(
		g: Graphics2D,
		message: String,
	) {
		g.font = font(Font.BOLD, 22f)
		g.color = Palette.TEXT_MUTED
		val width = g.fontMetrics.stringWidth(message)
		g.drawString(message, (CANVAS_WIDTH - width) / 2, (PLOT_TOP + PLOT_BOTTOM) / 2)
	}

	private fun drawPill(
		g: Graphics2D,
		text: String,
		x: Int,
		y: Int,
		textColor: Color,
		background: Color,
	) {
		g.font = font(Font.BOLD, 12f)
		val width = g.fontMetrics.stringWidth(text) + 18
		g.color = background
		g.fillRoundRect(x, y, width, 23, 12, 12)
		g.color = textColor
		g.drawString(text, x + 9, y + 16)
	}

	private fun linePath(points: List<ChartPoint>): Path2D.Double {
		val path = Path2D.Double()
		path.moveTo(points.first().x, points.first().y)
		points.drop(1).forEach { point -> path.lineTo(point.x, point.y) }
		return path
	}

	private fun stepsStats(points: List<StepPoint>): List<Stat> {
		val total = points.sumOf { it.steps.toLong() }
		val average = (total.toDouble() / points.size).roundToInt()
		val targetDays = points.count { it.steps >= STEP_TARGET }
		val best = points.maxBy { it.steps }
		val recent = points.takeLast(7).map { it.steps }.average()
		val previous = points.dropLast(7).takeLast(7).map { it.steps }.average()
		val weekDelta =
			if (previous.isNaN() || previous == 0.0) {
				"—"
			} else {
				formatPercent((recent - previous) / previous * 100)
			}
		return listOf(
			Stat("Среднее", formatInteger(average), Palette.TEXT),
			Stat("Цель", "$targetDays из ${points.size}", Palette.TARGET),
			Stat("Лучший день", formatInteger(best.steps), Palette.STEPS),
			Stat("Неделя", weekDelta, stepsDeltaColor(weekDelta)),
		)
	}

	private fun weightStats(points: List<WeightPoint>): List<Stat> {
		val first = points.first().weightKg
		val latest = points.last().weightKg
		val delta = latest - first
		return listOf(
			Stat("Изменение", formatWeightDelta(delta), weightDeltaColor(delta)),
			Stat("Минимум", "${formatDecimal(points.minOf { it.weightKg })} кг", Palette.TEXT),
			Stat("Максимум", "${formatDecimal(points.maxOf { it.weightKg })} кг", Palette.TEXT),
			Stat("Замеров", points.size.toString(), Palette.WEIGHT_LINE),
		)
	}

	private fun periodSubtitle(
		from: LocalDate,
		to: LocalDate,
		count: Int,
		countLabel: String,
	): String = "${shortDate(from)} — ${shortDate(to)}  ·  $count $countLabel"

	private fun canvas(): BufferedImage =
		BufferedImage(CANVAS_WIDTH, CANVAS_HEIGHT, BufferedImage.TYPE_INT_RGB).also { image ->
			val g = image.createGraphics()
			g.color = Palette.BG
			g.fillRect(0, 0, CANVAS_WIDTH, CANVAS_HEIGHT)
			g.dispose()
		}

	private fun BufferedImage.cleanGraphics(): Graphics2D =
		(createGraphics() as Graphics2D).apply {
			setRenderingHint(RenderingHints.KEY_ANTIALIASING, RenderingHints.VALUE_ANTIALIAS_ON)
			setRenderingHint(RenderingHints.KEY_TEXT_ANTIALIASING, RenderingHints.VALUE_TEXT_ANTIALIAS_ON)
			setRenderingHint(RenderingHints.KEY_RENDERING, RenderingHints.VALUE_RENDER_QUALITY)
			setRenderingHint(RenderingHints.KEY_STROKE_CONTROL, RenderingHints.VALUE_STROKE_PURE)
		}

	private fun yFor(
		value: Double,
		min: Double,
		max: Double,
	): Int {
		val fraction = ((value - min) / (max - min)).coerceIn(0.0, 1.0)
		return PLOT_BOTTOM - (PLOT_HEIGHT * fraction).roundToInt()
	}

	private fun drawRight(
		g: Graphics2D,
		text: String,
		right: Int,
		baseline: Int,
	) {
		g.drawString(text, right - g.fontMetrics.stringWidth(text), baseline)
	}

	private fun dashedStroke(
		width: Float,
		dash: Float,
		gap: Float,
	): BasicStroke =
		BasicStroke(
			width,
			BasicStroke.CAP_ROUND,
			BasicStroke.JOIN_ROUND,
			1f,
			floatArrayOf(dash, gap),
			0f,
		)

	private fun encode(image: BufferedImage): ByteArray =
		ByteArrayOutputStream().use { output ->
			check(ImageIO.write(image, "png", output)) { "PNG writer is unavailable" }
			output.toByteArray()
		}

	private fun formatInteger(value: Number): String =
		"%,d".format(java.util.Locale.US, value.toLong()).replace(',', ' ')

	private fun formatDecimal(value: Double): String =
		"%.1f".format(java.util.Locale.US, value).replace('.', ',')

	private fun formatWeightDelta(delta: Double): String =
		when {
			delta > 0 -> "+${formatDecimal(delta)} кг"
			delta < 0 -> "${formatDecimal(delta)} кг"
			else -> "±0 кг"
		}

	private fun formatPercent(value: Double): String =
		when {
			value > 0 -> "+${value.roundToInt()}%"
			value < 0 -> "${value.roundToInt()}%"
			else -> "±0%"
		}

	private fun stepsDeltaColor(value: String): Color =
		when {
			value.startsWith("+") -> Palette.POSITIVE
			value.startsWith("-") -> Palette.NEGATIVE
			else -> Palette.TEXT_MUTED
		}

	private fun weightDeltaColor(value: Double): Color =
		when {
			value > 0 -> Palette.NEGATIVE
			value < 0 -> Palette.LATEST
			else -> Palette.TEXT_MUTED
		}

	private fun shortDate(date: LocalDate): String =
		"${date.dayOfMonth} ${MONTHS[date.monthValue - 1]}"

	private fun font(
		style: Int,
		size: Float,
	): Font =
		listOf("DejaVu Sans", "SansSerif")
			.asSequence()
			.map { Font(it, style, size.roundToInt()) }
			.firstOrNull { it.canDisplay('Я') && it.canDisplay('ж') }
			?.deriveFont(style, size)
			?: Font(Font.SANS_SERIF, style, size.roundToInt()).deriveFont(style, size)

	private data class Stat(
		val label: String,
		val value: String,
		val color: Color,
	)

	private data class ChartPoint(
		val x: Double,
		val y: Double,
	)

	private object Palette {
		val BG = Color(5, 13, 25)
		val TEXT = Color(238, 242, 247)
		val TEXT_MUTED = Color(145, 158, 176)
		val TEXT_DIM = Color(105, 121, 143)
		val GRID = Color(71, 85, 105, 55)
		val SEPARATOR = Color(71, 85, 105, 90)
		val PILL_DARK = Color(13, 25, 42, 230)
		val STEPS = Color(45, 196, 154)
		val STEPS_MUTED = Color(32, 137, 119)
		val STEPS_HIGH = Color(53, 190, 151)
		val STEPS_LATEST = Color(103, 232, 190)
		val TARGET = Color(250, 204, 21)
		val TARGET_LINE = Color(250, 204, 21, 150)
		val WEIGHT_LINE = Color(244, 176, 62)
		val WEIGHT_FILL_TOP = Color(239, 68, 68, 175)
		val WEIGHT_FILL_BOTTOM = Color(94, 29, 42, 25)
		val AVERAGE_LINE = Color(203, 213, 225, 90)
		val LATEST = Color(125, 211, 252)
		val POSITIVE = Color(52, 211, 153)
		val NEGATIVE = Color(251, 113, 133)
	}

	private companion object {
		val MONTHS = listOf("янв", "фев", "мар", "апр", "мая", "июн", "июл", "авг", "сен", "окт", "ноя", "дек")
		const val STEP_TARGET = 10_000
		const val CANVAS_WIDTH = 1200
		const val CANVAS_HEIGHT = 760
		const val CONTENT_LEFT = 54
		const val CONTENT_RIGHT = 1146
		const val PLOT_LEFT = 72
		const val PLOT_RIGHT = 1098
		const val PLOT_TOP = 122
		const val PLOT_BOTTOM = 610
		const val PLOT_WIDTH = PLOT_RIGHT - PLOT_LEFT
		const val PLOT_HEIGHT = PLOT_BOTTOM - PLOT_TOP
		const val GRID_LINES = 4
		const val STATS_TOP = 666
	}
}
