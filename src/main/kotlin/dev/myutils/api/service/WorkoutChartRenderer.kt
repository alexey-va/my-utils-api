package dev.myutils.api.service

import org.knowm.xchart.BitmapEncoder
import org.knowm.xchart.XYChart
import org.knowm.xchart.XYChartBuilder
import org.knowm.xchart.style.markers.SeriesMarkers
import org.springframework.stereotype.Service
import java.awt.Font
import java.io.ByteArrayOutputStream
import java.time.LocalDate
import java.time.ZoneId
import java.util.Date
import javax.imageio.ImageIO

/** PNG-графики прогресса (headless, без Python). */
@Service
class WorkoutChartRenderer {
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
		val xDates = sorted.map { it.date.toChartDate() }
		val weights = sorted.map { it.weightKg.toDouble() }
		val maxReps = sorted.map { it.maxReps.toDouble() }

		val chart: XYChart =
			XYChartBuilder()
				.width(CHART_WIDTH)
				.height(CHART_HEIGHT)
				.title("«$exerciseName» — прогресс")
				.xAxisTitle("Дата")
				.yAxisTitle("Вес, кг")
				.build()

		applyStyle(chart)
		chart.addSeries("Вес", xDates, weights).marker = SeriesMarkers.CIRCLE
		val maxSeries = chart.addSeries("МАХ повторы", xDates, maxReps)
		maxSeries.marker = SeriesMarkers.DIAMOND
		maxSeries.lineStyle = java.awt.BasicStroke(1.5f, java.awt.BasicStroke.CAP_ROUND, java.awt.BasicStroke.JOIN_ROUND, 1f, floatArrayOf(6f, 4f), 0f)
		maxSeries.yAxisGroup = 1
		chart.styler.setYAxisGroupPosition(1, org.knowm.xchart.style.Styler.YAxisPosition.Right)
		chart.setYAxisGroupTitle(1, "МАХ")

		val image = BitmapEncoder.getBufferedImage(chart)
		val output = ByteArrayOutputStream()
		ImageIO.write(image, "png", output)
		return output.toByteArray()
	}

	private fun applyStyle(chart: XYChart) {
		val font = chartFont()
		chart.styler.apply {
			isLegendVisible = true
			legendPosition = org.knowm.xchart.style.Styler.LegendPosition.InsideNE
			isPlotGridLinesVisible = true
			plotBackgroundColor = java.awt.Color.WHITE
			chartBackgroundColor = java.awt.Color.WHITE
			setAxisTitleFont(font.deriveFont(Font.BOLD, 13f))
			setAxisTickLabelsFont(font.deriveFont(11f))
			setLegendFont(font.deriveFont(11f))
			setChartTitleFont(font.deriveFont(Font.BOLD, 15f))
		}
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

	companion object {
		private const val CHART_WIDTH = 900
		private const val CHART_HEIGHT = 520
	}
}
