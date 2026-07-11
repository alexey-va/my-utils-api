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
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Date
import javax.imageio.ImageIO

/** PNG-графики прогресса (headless, без Python). */
@Service
class WorkoutChartRenderer {
	private val dateFmt = DateTimeFormatter.ofPattern("dd.MM.yy")

	data class Point(
		val date: LocalDate,
		val weightKg: Int,
		val maxReps: Int,
	)

	fun renderWeightProgress(
		exerciseName: String,
		points: List<Point>,
	): ByteArray {
		require(points.size >= 2) { "Нужно минимум 2 сессии для графика" }
		val sorted = points.sortedBy { it.date }
		val chartImage = renderPlot(sorted)
		val framed = composeFrame(chartImage, exerciseName, sorted)
		val output = ByteArrayOutputStream()
		ImageIO.write(framed, "png", output)
		return output.toByteArray()
	}

	private fun renderPlot(sorted: List<Point>): BufferedImage {
		val xDates = sorted.map { it.date.toChartDate() }
		val weights = sorted.map { it.weightKg.toDouble() }
		val maxReps = sorted.map { it.maxReps.toDouble() }

		val chart: XYChart =
			XYChartBuilder()
				.width(PLOT_WIDTH)
				.height(PLOT_HEIGHT)
				.title("")
				.xAxisTitle("")
				.yAxisTitle("Вес, кг")
				.build()

		applyPlotStyle(chart, sorted)
		val weightSeries = chart.addSeries("Вес", xDates, weights)
		styleWeightSeries(weightSeries)

		val maxSeries = chart.addSeries("МАХ", xDates, maxReps)
		styleMaxSeries(maxSeries)
		maxSeries.yAxisGroup = 1
		chart.styler.setYAxisGroupPosition(1, Styler.YAxisPosition.Right)
		chart.setYAxisGroupTitle(1, "МАХ повторы")

		return BitmapEncoder.getBufferedImage(chart)
	}

	private fun applyPlotStyle(
		chart: XYChart,
		sorted: List<Point>,
	) {
		val font = chartFont()
		val zone = ZoneId.systemDefault()
		chart.styler.apply {
			isChartTitleVisible = false
			chartBackgroundColor = Palette.CARD
			plotBackgroundColor = Palette.PLOT
			plotBorderColor = Palette.PLOT_BORDER
			isPlotBorderVisible = true
			chartFontColor = Palette.TEXT
			setChartPadding(8)
			setPlotMargin(14)

			isLegendVisible = true
			legendPosition = Styler.LegendPosition.InsideNW
			legendBackgroundColor = Palette.LEGEND_BG
			legendBorderColor = Palette.LEGEND_BORDER
			setLegendPadding(10)
			setLegendSeriesLineLength(14)
			setLegendFont(font.deriveFont(Font.PLAIN, 12f))

			isPlotGridLinesVisible = true
			isPlotGridVerticalLinesVisible = false
			plotGridLinesColor = Palette.GRID
			plotGridLinesStroke = BasicStroke(1f, BasicStroke.CAP_ROUND, BasicStroke.JOIN_ROUND, 1f, floatArrayOf(4f, 6f), 0f)

			setAxisTitleFont(font.deriveFont(Font.BOLD, 12f))
			setAxisTickLabelsFont(font.deriveFont(Font.PLAIN, 11f))
			axisTickLabelsColor = Palette.TEXT_MUTED
			axisTickMarksColor = Palette.GRID
			yAxisTitleColor = Palette.WEIGHT
			setYAxisGroupTitleColor(1, Palette.MAX)

			setSeriesColors(arrayOf(Palette.WEIGHT, Palette.MAX))
			setxAxisTickLabelsFormattingFunction { value ->
				val millis = value.toLong()
				dateFmt.format(
					java.time.Instant
						.ofEpochMilli(millis)
						.atZone(zone)
						.toLocalDate(),
				)
			}
		}
	}

	private fun styleWeightSeries(series: XYSeries) {
		series.apply {
			xySeriesRenderStyle = XYSeries.XYSeriesRenderStyle.Area
			isSmooth = true
			lineColor = Palette.WEIGHT
			lineWidth = 3f
			fillColor = Palette.WEIGHT_FILL
			marker = SeriesMarkers.CIRCLE
			markerColor = Palette.WEIGHT
			lineStyle = BasicStroke(3f, BasicStroke.CAP_ROUND, BasicStroke.JOIN_ROUND)
		}
	}

	private fun styleMaxSeries(series: XYSeries) {
		series.apply {
			xySeriesRenderStyle = XYSeries.XYSeriesRenderStyle.Line
			isSmooth = true
			lineColor = Palette.MAX
			lineWidth = 2.5f
			marker = SeriesMarkers.DIAMOND
			markerColor = Palette.MAX
			lineStyle =
				BasicStroke(
					2.5f,
					BasicStroke.CAP_ROUND,
					BasicStroke.JOIN_ROUND,
					1f,
					floatArrayOf(8f, 5f),
					0f,
				)
		}
	}

	private fun composeFrame(
		plot: BufferedImage,
		exerciseName: String,
		points: List<Point>,
	): BufferedImage {
		val image = BufferedImage(CANVAS_WIDTH, CANVAS_HEIGHT, BufferedImage.TYPE_INT_RGB)
		val g = image.createGraphics() as Graphics2D
		g.setRenderingHint(RenderingHints.KEY_ANTIALIASING, RenderingHints.VALUE_ANTIALIAS_ON)
		g.setRenderingHint(RenderingHints.KEY_TEXT_ANTIALIASING, RenderingHints.VALUE_TEXT_ANTIALIAS_ON)

		val bg =
			GradientPaint(
				0f,
				0f,
				Palette.BG_TOP,
				0f,
				CANVAS_HEIGHT.toFloat(),
				Palette.BG_BOTTOM,
			)
		g.paint = bg
		g.fillRect(0, 0, CANVAS_WIDTH, CANVAS_HEIGHT)

		val cardX = FRAME_PADDING
		val cardY = FRAME_PADDING
		val cardW = CANVAS_WIDTH - FRAME_PADDING * 2
		val cardH = CANVAS_HEIGHT - FRAME_PADDING * 2
		g.color = Palette.CARD
		g.fillRoundRect(cardX, cardY, cardW, cardH, 28, 28)

		val headerH = 96
		val header =
			GradientPaint(
				cardX.toFloat(),
				cardY.toFloat(),
				Palette.HEADER_LEFT,
				(cardX + cardW).toFloat(),
				cardY.toFloat(),
				Palette.HEADER_RIGHT,
			)
		g.paint = header
		g.fillRoundRect(cardX, cardY, cardW, headerH, 28, 28)
		g.fillRect(cardX, cardY + headerH - 28, cardW, 28)

		val titleFont = chartFont().deriveFont(Font.BOLD, 22f)
		val subtitleFont = chartFont().deriveFont(Font.PLAIN, 13f)
		g.font = titleFont
		g.color = Color.WHITE
		g.drawString(exerciseName, cardX + 24, cardY + 40)
		g.font = subtitleFont
		g.color = Palette.HEADER_SUBTITLE
		g.drawString(buildSubtitle(points), cardX + 24, cardY + 68)

		val plotX = cardX + 12
		val plotY = cardY + headerH + 4
		g.drawImage(plot, plotX, plotY, null)

		drawStatsBar(g, cardX, cardY + cardH - 56, cardW, points)

		g.dispose()
		return image
	}

	private fun drawStatsBar(
		g: Graphics2D,
		x: Int,
		y: Int,
		width: Int,
		points: List<Point>,
	) {
		val first = points.first()
		val last = points.last()
		val weightDelta = last.weightKg - first.weightKg
		val bestMax = points.maxOf { it.maxReps }
		val bestWeight = points.maxOf { it.weightKg }

		g.color = Palette.STATS_BG
		g.fillRoundRect(x + 16, y, width - 32, 44, 16, 16)

		val font = chartFont().deriveFont(Font.PLAIN, 13f)
		val bold = chartFont().deriveFont(Font.BOLD, 13f)
		var cursorX = x + 32

		cursorX = drawStatChip(g, cursorX, y + 14, "Вес", font, Palette.TEXT_MUTED)
		cursorX = drawStatValue(g, cursorX + 6, y + 14, "${first.weightKg}", bold, Palette.WEIGHT)
		cursorX = drawStatValue(g, cursorX + 8, y + 14, "→", font, Palette.TEXT_MUTED)
		cursorX = drawStatValue(g, cursorX + 8, y + 14, "${last.weightKg} кг", bold, Palette.TEXT)
		val deltaText = formatDelta(weightDelta, "кг")
		cursorX = drawStatValue(g, cursorX + 10, y + 14, deltaText, bold, deltaColor(weightDelta))

		cursorX += 28
		cursorX = drawStatChip(g, cursorX, y + 14, "Рекорд", font, Palette.TEXT_MUTED)
		drawStatValue(g, cursorX + 6, y + 14, "$bestWeight кг / МАХ $bestMax", bold, Palette.TEXT)
	}

	private fun drawStatChip(
		g: Graphics2D,
		x: Int,
		y: Int,
		text: String,
		font: Font,
		color: Color,
	): Int {
		g.font = font
		g.color = color
		g.drawString(text, x, y + font.size2D.toInt())
		return x + g.fontMetrics.stringWidth(text)
	}

	private fun drawStatValue(
		g: Graphics2D,
		x: Int,
		y: Int,
		text: String,
		font: Font,
		color: Color,
	): Int {
		g.font = font
		g.color = color
		g.drawString(text, x, y + font.size2D.toInt())
		return x + g.fontMetrics.stringWidth(text)
	}

	private fun buildSubtitle(points: List<Point>): String {
		val from = dateFmt.format(points.first().date)
		val to = dateFmt.format(points.last().date)
		val sessions = points.size
		val sessionWord =
			when {
				sessions % 100 in 11..14 -> "сессий"
				sessions % 10 == 1 -> "сессия"
				sessions % 10 in 2..4 -> "сессии"
				else -> "сессий"
			}
		return "$sessions $sessionWord · $from — $to"
	}

	private fun formatDelta(
		delta: Int,
		unit: String,
	): String =
		when {
			delta > 0 -> "(+$delta $unit)"
			delta < 0 -> "($delta $unit)"
			else -> "(±0 $unit)"
		}

	private fun deltaColor(delta: Int): Color =
		when {
			delta > 0 -> Palette.POSITIVE
			delta < 0 -> Palette.NEGATIVE
			else -> Palette.TEXT_MUTED
		}

	private fun chartFont(): Font {
		val candidates = listOf("DejaVu Sans", "SansSerif")
		for (name in candidates) {
			val font = Font(name, Font.PLAIN, 12)
			if (font.canDisplay('ж') && font.canDisplay('Я')) {
				return font
			}
		}
		return Font(Font.SANS_SERIF, Font.PLAIN, 12)
	}

	private fun LocalDate.toChartDate(): Date =
		Date.from(atStartOfDay(ZoneId.systemDefault()).toInstant())

	private object Palette {
		val BG_TOP = Color(15, 23, 42)
		val BG_BOTTOM = Color(30, 41, 59)
		val CARD = Color(30, 37, 56)
		val PLOT = Color(24, 30, 46)
		val PLOT_BORDER = Color(51, 65, 85)
		val HEADER_LEFT = Color(37, 99, 235)
		val HEADER_RIGHT = Color(124, 58, 237)
		val HEADER_SUBTITLE = Color(219, 234, 254)
		val TEXT = Color(226, 232, 240)
		val TEXT_MUTED = Color(148, 163, 184)
		val WEIGHT = Color(59, 130, 246)
		val WEIGHT_FILL = Color(59, 130, 246, 55)
		val MAX = Color(251, 191, 36)
		val GRID = Color(51, 65, 85, 120)
		val LEGEND_BG = Color(15, 23, 42, 170)
		val LEGEND_BORDER = Color(71, 85, 105)
		val STATS_BG = Color(15, 23, 42, 190)
		val POSITIVE = Color(52, 211, 153)
		val NEGATIVE = Color(248, 113, 113)
	}

	companion object {
		private const val CANVAS_WIDTH = 1000
		private const val CANVAS_HEIGHT = 640
		private const val PLOT_WIDTH = 960
		private const val PLOT_HEIGHT = 460
		private const val FRAME_PADDING = 20
	}
}
