package dev.myutils.api.service

import org.knowm.xchart.BitmapEncoder
import org.knowm.xchart.XYChart
import org.knowm.xchart.XYChartBuilder
import org.knowm.xchart.XYSeries
import org.knowm.xchart.style.Styler
import org.knowm.xchart.style.markers.SeriesMarkers
import org.springframework.stereotype.Service
import java.awt.BasicStroke
import java.awt.Color
import java.awt.Font
import java.awt.GradientPaint
import java.awt.Graphics2D
import java.awt.RenderingHints
import java.awt.image.BufferedImage
import java.io.ByteArrayOutputStream
import java.time.LocalDate
import java.time.ZoneOffset
import java.time.format.DateTimeFormatter
import java.util.Date
import javax.imageio.ImageIO
import kotlin.math.roundToInt

/** Detailed weekly snapshots of the public Workout health charts. */
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
		val sorted = points.sortedBy { it.date }
		val chart =
			if (sorted.isEmpty()) {
				emptyPlot("За выбранный период шаги не загружены")
			} else {
				stepsPlot(sorted)
			}
		val stats =
			if (sorted.isEmpty()) {
				listOf(Stat("Данные", "пока пусто", Palette.TEXT_MUTED))
			} else {
				stepsStats(sorted)
			}
		return encode(
			composeFrame(
				title = "Шаги",
				subtitle = "Динамика активности · ${dateFmt.format(from)} — ${dateFmt.format(to)}",
				plot = chart,
				stats = stats,
				accent = Palette.STEPS,
			),
		)
	}

	fun renderWeight(
		points: List<WeightPoint>,
		from: LocalDate,
		to: LocalDate,
	): ByteArray {
		val sorted = points.sortedBy { it.date }
		val chart =
			if (sorted.isEmpty()) {
				emptyPlot("За выбранный период вес не записывали")
			} else {
				weightPlot(sorted)
			}
		val stats =
			if (sorted.isEmpty()) {
				listOf(Stat("Данные", "пока пусто", Palette.TEXT_MUTED))
			} else {
				weightStats(sorted)
			}
		return encode(
			composeFrame(
				title = "Вес тела",
				subtitle = "Тренд и диапазон · ${dateFmt.format(from)} — ${dateFmt.format(to)}",
				plot = chart,
				stats = stats,
				accent = Palette.WEIGHT,
			),
		)
	}

	private fun stepsPlot(points: List<StepPoint>): BufferedImage {
		val dates = points.map { it.date.toChartDate() }
		val values = points.map { it.steps.toDouble() }
		val chart = baseChart("Шаги")
		chart.styler.apply {
			setSeriesColors(arrayOf(Palette.STEPS, Palette.TARGET))
			isLegendVisible = true
		}
		chart
			.addSeries("Шаги", dates, values)
			.apply {
				xySeriesRenderStyle = XYSeries.XYSeriesRenderStyle.Area
				fillColor = Palette.STEPS_FILL
				lineColor = Palette.STEPS
				lineWidth = 2.5f
				lineStyle = BasicStroke(2.5f, BasicStroke.CAP_ROUND, BasicStroke.JOIN_ROUND)
				marker = SeriesMarkers.CIRCLE
				markerColor = Palette.STEPS
			}
		chart
			.addSeries("Цель 10 000", dates, List(points.size) { STEP_TARGET.toDouble() })
			.apply {
				xySeriesRenderStyle = XYSeries.XYSeriesRenderStyle.Line
				lineColor = Palette.TARGET
				lineWidth = 2f
				lineStyle =
					BasicStroke(
						2f,
						BasicStroke.CAP_ROUND,
						BasicStroke.JOIN_ROUND,
						1f,
						floatArrayOf(8f, 6f),
						0f,
					)
				marker = SeriesMarkers.NONE
			}
		return BitmapEncoder.getBufferedImage(chart)
	}

	private fun weightPlot(points: List<WeightPoint>): BufferedImage {
		val chart = baseChart("Вес, кг")
		chart.styler.apply {
			setSeriesColors(arrayOf(Palette.WEIGHT))
			isLegendVisible = false
		}
		chart
			.addSeries(
				"Вес",
				points.map { it.date.toChartDate() },
				points.map { it.weightKg },
			).apply {
				xySeriesRenderStyle = XYSeries.XYSeriesRenderStyle.Area
				isSmooth = true
				lineColor = Palette.WEIGHT
				lineWidth = 3f
				lineStyle = BasicStroke(3f, BasicStroke.CAP_ROUND, BasicStroke.JOIN_ROUND)
				fillColor = Palette.WEIGHT_FILL
				marker = SeriesMarkers.CIRCLE
				markerColor = Palette.WEIGHT
			}
		val min = points.minOf { it.weightKg }
		val max = points.maxOf { it.weightKg }
		val padding = ((max - min) * 0.3).coerceAtLeast(0.7)
		chart.styler.yAxisMin = min - padding
		chart.styler.yAxisMax = max + padding
		return BitmapEncoder.getBufferedImage(chart)
	}

	private fun baseChart(yAxisTitle: String): XYChart =
		XYChartBuilder()
			.width(PLOT_WIDTH)
			.height(PLOT_HEIGHT)
			.title("")
			.xAxisTitle("")
			.yAxisTitle(yAxisTitle)
			.build()
			.also { chart ->
				chart.styler.apply {
					isChartTitleVisible = false
					chartBackgroundColor = Palette.CARD
					plotBackgroundColor = Palette.PLOT
					plotBorderColor = Palette.PLOT_BORDER
					isPlotBorderVisible = true
					chartFontColor = Palette.TEXT
					setChartPadding(10)
					setPlotMargin(12)
					isPlotGridLinesVisible = true
					isPlotGridVerticalLinesVisible = false
					plotGridLinesColor = Palette.GRID
					plotGridLinesStroke =
						BasicStroke(
							1f,
							BasicStroke.CAP_ROUND,
							BasicStroke.JOIN_ROUND,
							1f,
							floatArrayOf(4f, 6f),
							0f,
						)
					setAxisTitleFont(chartFont().deriveFont(Font.BOLD, 14f))
					setAxisTickLabelsFont(chartFont().deriveFont(Font.PLAIN, 12f))
					axisTickLabelsColor = Palette.TEXT_MUTED
					axisTickMarksColor = Palette.GRID
					setLegendFont(chartFont().deriveFont(Font.PLAIN, 12f))
					legendPosition = Styler.LegendPosition.InsideNW
					legendBackgroundColor = Palette.LEGEND_BG
					legendBorderColor = Palette.LEGEND_BORDER
					setxAxisTickLabelsFormattingFunction { value ->
						dateFmt.format(
							java.time.Instant
								.ofEpochMilli(value.toLong())
								.atZone(ZoneOffset.UTC)
								.toLocalDate(),
						)
					}
				}
			}

	private fun emptyPlot(message: String): BufferedImage {
		val image = BufferedImage(PLOT_WIDTH, PLOT_HEIGHT, BufferedImage.TYPE_INT_RGB)
		val g = image.createGraphics()
		g.color = Palette.PLOT
		g.fillRect(0, 0, image.width, image.height)
		g.font = chartFont().deriveFont(Font.BOLD, 22f)
		g.color = Palette.TEXT_MUTED
		val width = g.fontMetrics.stringWidth(message)
		g.drawString(message, (image.width - width) / 2, image.height / 2)
		g.dispose()
		return image
	}

	private fun composeFrame(
		title: String,
		subtitle: String,
		plot: BufferedImage,
		stats: List<Stat>,
		accent: Color,
	): BufferedImage {
		val image = BufferedImage(CANVAS_WIDTH, CANVAS_HEIGHT, BufferedImage.TYPE_INT_RGB)
		val g = image.createGraphics() as Graphics2D
		g.setRenderingHint(RenderingHints.KEY_ANTIALIASING, RenderingHints.VALUE_ANTIALIAS_ON)
		g.setRenderingHint(RenderingHints.KEY_TEXT_ANTIALIASING, RenderingHints.VALUE_TEXT_ANTIALIAS_ON)
		g.paint =
			GradientPaint(
				0f,
				0f,
				Palette.BG_TOP,
				CANVAS_WIDTH.toFloat(),
				CANVAS_HEIGHT.toFloat(),
				Palette.BG_BOTTOM,
			)
		g.fillRect(0, 0, CANVAS_WIDTH, CANVAS_HEIGHT)

		g.color = Palette.CARD
		g.fillRoundRect(FRAME, FRAME, CANVAS_WIDTH - FRAME * 2, CANVAS_HEIGHT - FRAME * 2, 32, 32)
		g.paint =
			GradientPaint(
				FRAME.toFloat(),
				FRAME.toFloat(),
				accent,
				(CANVAS_WIDTH - FRAME).toFloat(),
				FRAME.toFloat(),
				Palette.HEADER_END,
			)
		g.fillRoundRect(FRAME, FRAME, CANVAS_WIDTH - FRAME * 2, HEADER_HEIGHT, 32, 32)
		g.fillRect(FRAME, FRAME + HEADER_HEIGHT - 32, CANVAS_WIDTH - FRAME * 2, 32)

		g.font = chartFont().deriveFont(Font.BOLD, 30f)
		g.color = Color.WHITE
		g.drawString(title, FRAME + 34, FRAME + 50)
		g.font = chartFont().deriveFont(Font.PLAIN, 15f)
		g.color = Palette.HEADER_SUBTITLE
		g.drawString(subtitle, FRAME + 34, FRAME + 80)
		g.font = chartFont().deriveFont(Font.BOLD, 13f)
		g.drawString("ЕЖЕНЕДЕЛЬНЫЙ ОТЧЁТ", CANVAS_WIDTH - FRAME - 205, FRAME + 50)

		g.drawImage(plot, FRAME + 20, FRAME + HEADER_HEIGHT + 10, null)
		drawStats(g, stats)
		g.dispose()
		return image
	}

	private fun drawStats(
		g: Graphics2D,
		stats: List<Stat>,
	) {
		val y = CANVAS_HEIGHT - FRAME - 74
		val x = FRAME + 26
		val width = CANVAS_WIDTH - FRAME * 2 - 52
		g.color = Palette.STATS_BG
		g.fillRoundRect(x, y, width, 54, 18, 18)

		val cellWidth = width / stats.size.coerceAtLeast(1)
		stats.forEachIndexed { index, stat ->
			val cellX = x + index * cellWidth + 18
			g.font = chartFont().deriveFont(Font.PLAIN, 12f)
			g.color = Palette.TEXT_MUTED
			g.drawString(stat.label.uppercase(), cellX, y + 20)
			g.font = chartFont().deriveFont(Font.BOLD, 16f)
			g.color = stat.color
			g.drawString(stat.value, cellX, y + 42)
			if (index > 0) {
				g.color = Palette.PLOT_BORDER
				g.drawLine(x + index * cellWidth, y + 11, x + index * cellWidth, y + 43)
			}
		}
	}

	private fun stepsStats(points: List<StepPoint>): List<Stat> {
		val total = points.sumOf { it.steps.toLong() }
		val average = (total.toDouble() / points.size).roundToInt()
		val targetDays = points.count { it.steps >= STEP_TARGET }
		val recent = points.takeLast(7).map { it.steps }.average()
		val previous = points.dropLast(7).takeLast(7).map { it.steps }.average()
		val weekDelta =
			if (previous.isNaN() || previous == 0.0) {
				"—"
			} else {
				formatPercent((recent - previous) / previous * 100)
			}
		return listOf(
			Stat("Среднее / день", formatInteger(average), Palette.STEPS),
			Stat("Всего", formatInteger(total), Palette.TEXT),
			Stat("Цель 10 000", "$targetDays / ${points.size} дней", Palette.TARGET),
			Stat("Неделя к неделе", weekDelta, deltaColor(weekDelta)),
		)
	}

	private fun weightStats(points: List<WeightPoint>): List<Stat> {
		val first = points.first().weightKg
		val latest = points.last().weightKg
		val delta = latest - first
		return listOf(
			Stat("Последний", "${formatDecimal(latest)} кг", Palette.WEIGHT),
			Stat("Изменение", formatWeightDelta(delta), deltaColor(delta)),
			Stat("Минимум", "${formatDecimal(points.minOf { it.weightKg })} кг", Palette.TEXT),
			Stat("Максимум", "${formatDecimal(points.maxOf { it.weightKg })} кг", Palette.TEXT),
			Stat("Замеров", points.size.toString(), Palette.TEXT_MUTED),
		)
	}

	private fun encode(image: BufferedImage): ByteArray =
		ByteArrayOutputStream().use { output ->
			check(ImageIO.write(image, "png", output)) { "PNG writer is unavailable" }
			output.toByteArray()
		}

	private fun LocalDate.toChartDate(): Date =
		Date.from(atStartOfDay(ZoneOffset.UTC).toInstant())

	private fun formatInteger(value: Number): String =
		"%,d".format(java.util.Locale.US, value.toLong()).replace(',', ' ')

	private fun formatDecimal(value: Double): String =
		"%.1f".format(java.util.Locale.US, value)

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

	private fun deltaColor(value: Double): Color =
		when {
			value > 0 -> Palette.POSITIVE
			value < 0 -> Palette.NEGATIVE
			else -> Palette.TEXT_MUTED
		}

	private fun deltaColor(value: String): Color =
		when {
			value.startsWith("+") -> Palette.POSITIVE
			value.startsWith("-") -> Palette.NEGATIVE
			else -> Palette.TEXT_MUTED
		}

	private fun chartFont(): Font =
		listOf("DejaVu Sans", "SansSerif")
			.asSequence()
			.map { Font(it, Font.PLAIN, 12) }
			.firstOrNull { it.canDisplay('Я') && it.canDisplay('ж') }
			?: Font(Font.SANS_SERIF, Font.PLAIN, 12)

	private data class Stat(
		val label: String,
		val value: String,
		val color: Color,
	)

	private object Palette {
		val BG_TOP = Color(15, 23, 42)
		val BG_BOTTOM = Color(35, 26, 66)
		val CARD = Color(30, 37, 56)
		val PLOT = Color(24, 30, 46)
		val PLOT_BORDER = Color(51, 65, 85)
		val HEADER_END = Color(109, 93, 252)
		val HEADER_SUBTITLE = Color(235, 232, 255)
		val TEXT = Color(226, 232, 240)
		val TEXT_MUTED = Color(148, 163, 184)
		val GRID = Color(71, 85, 105, 120)
		val LEGEND_BG = Color(15, 23, 42, 200)
		val LEGEND_BORDER = Color(71, 85, 105)
		val STATS_BG = Color(15, 23, 42, 220)
		val STEPS = Color(52, 211, 153)
		val STEPS_FILL = Color(52, 211, 153, 190)
		val TARGET = Color(251, 191, 36)
		val WEIGHT = Color(155, 138, 251)
		val WEIGHT_FILL = Color(155, 138, 251, 65)
		val POSITIVE = Color(52, 211, 153)
		val NEGATIVE = Color(248, 113, 113)
	}

	companion object {
		private val dateFmt = DateTimeFormatter.ofPattern("dd.MM.yyyy")
		private const val STEP_TARGET = 10_000
		private const val CANVAS_WIDTH = 1200
		private const val CANVAS_HEIGHT = 760
		private const val PLOT_WIDTH = 1120
		private const val PLOT_HEIGHT = 500
		private const val FRAME = 20
		private const val HEADER_HEIGHT = 104
	}
}
