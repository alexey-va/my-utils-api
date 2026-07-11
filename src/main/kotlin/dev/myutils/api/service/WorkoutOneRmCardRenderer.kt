package dev.myutils.api.service

import org.springframework.stereotype.Service
import java.awt.BasicStroke
import java.awt.Color
import java.awt.Font
import java.awt.GradientPaint
import java.awt.Graphics2D
import java.awt.RenderingHints
import java.awt.geom.RoundRectangle2D
import java.awt.image.BufferedImage
import java.io.ByteArrayOutputStream
import java.text.DecimalFormat
import java.time.format.DateTimeFormatter
import javax.imageio.ImageIO

/** PNG-карточка оценки 1ПМ для Telegram. */
@Service
class WorkoutOneRmCardRenderer {
	private val dateFmt = DateTimeFormatter.ofPattern("dd.MM.yy")
	private val weightFmt = DecimalFormat("#0.#")

	fun render(report: OneRepMaxEstimator.Report): ByteArray {
		val image = BufferedImage(CANVAS_WIDTH, CANVAS_HEIGHT, BufferedImage.TYPE_INT_RGB)
		val g = image.createGraphics() as Graphics2D
		g.setRenderingHint(RenderingHints.KEY_ANTIALIASING, RenderingHints.VALUE_ANTIALIAS_ON)
		g.setRenderingHint(RenderingHints.KEY_TEXT_ANTIALIASING, RenderingHints.VALUE_TEXT_ANTIALIAS_ON)

		drawBackground(g)
		drawHeader(g, report)
		drawHero(g, report)
		drawFormulas(g, report)
		drawZones(g, report)
		drawFooter(g, report)

		g.dispose()
		val output = ByteArrayOutputStream()
		ImageIO.write(image, "png", output)
		return output.toByteArray()
	}

	private fun drawBackground(g: Graphics2D) {
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

		g.color = Palette.CARD
		g.fillRoundRect(PADDING, PADDING, CANVAS_WIDTH - PADDING * 2, CANVAS_HEIGHT - PADDING * 2, 28, 28)
	}

	private fun drawHeader(
		g: Graphics2D,
		report: OneRepMaxEstimator.Report,
	) {
		val x = PADDING
		val y = PADDING
		val w = CANVAS_WIDTH - PADDING * 2
		val h = 88
		val header =
			GradientPaint(
				x.toFloat(),
				y.toFloat(),
				Palette.HEADER_LEFT,
				(x + w).toFloat(),
				y.toFloat(),
				Palette.HEADER_RIGHT,
			)
		g.paint = header
		g.fillRoundRect(x, y, w, h, 28, 28)
		g.fillRect(x, y + h - 28, w, 28)

		g.font = font(Font.BOLD, 14f)
		g.color = Palette.HEADER_BADGE
		g.drawString("ОЦЕНКА 1ПМ", x + 24, y + 32)

		g.font = font(Font.BOLD, 24f)
		g.color = Color.WHITE
		g.drawString(report.exerciseName, x + 24, y + 62)

		g.font = font(Font.PLAIN, 13f)
		g.color = Palette.HEADER_SUBTITLE
		val date = dateFmt.format(report.session.date)
		g.drawString(date, x + w - 24 - g.fontMetrics.stringWidth(date), y + 62)
	}

	private fun drawHero(
		g: Graphics2D,
		report: OneRepMaxEstimator.Report,
	) {
		val centerY = 210
		val oneRm = report.session.consensusKg
		g.font = font(Font.BOLD, 72f)
		g.color = Palette.HERO
		val value = "${weightFmt.format(oneRm)} кг"
		val valueW = g.fontMetrics.stringWidth(value)
		g.drawString(value, (CANVAS_WIDTH - valueW) / 2, centerY)

		g.font = font(Font.PLAIN, 16f)
		g.color = Palette.TEXT_MUTED
		val set = report.session.bestSet
		val subtitle = "по подходу ${set.weightKg} × ${set.reps}  ·  ${confidenceRu(report.session.confidence)}"
		val subW = g.fontMetrics.stringWidth(subtitle)
		g.drawString(subtitle, (CANVAS_WIDTH - subW) / 2, centerY + 34)

		g.font = font(Font.PLAIN, 14f)
		g.color = Palette.TEXT
		val notation = report.session.notation
		val noteW = g.fontMetrics.stringWidth(notation)
		g.drawString(notation, (CANVAS_WIDTH - noteW) / 2, centerY + 58)
	}

	private fun drawFormulas(
		g: Graphics2D,
		report: OneRepMaxEstimator.Report,
	) {
		val y = 300
		g.font = font(Font.BOLD, 13f)
		g.color = Palette.TEXT_MUTED
		g.drawString("Формулы", PADDING + 28, y)

		var x = PADDING + 28
		val chipY = y + 14
		for (formula in report.session.formulas) {
			val label = "${formula.name} ${weightFmt.format(formula.oneRmKg)}"
			val chipW = g.fontMetrics.stringWidth(label) + 28
			drawChip(g, x, chipY, chipW, 34, label, Palette.CHIP_BG, Palette.CHIP_TEXT)
			x += chipW + 10
		}
	}

	private fun drawZones(
		g: Graphics2D,
		report: OneRepMaxEstimator.Report,
	) {
		val x = PADDING + 20
		val y = 370
		val w = CANVAS_WIDTH - (PADDING + 20) * 2
		val h = 250
		g.color = Palette.ZONE_PANEL
		g.fillRoundRect(x, y, w, h, 20, 20)

		g.font = font(Font.BOLD, 14f)
		g.color = Palette.TEXT
		g.drawString("Рабочие зоны от 1ПМ", x + 20, y + 28)

		var rowY = y + 52
		val barX = x + 170
		val barMaxW = w - 210
		for (zone in report.zones) {
			g.font = font(Font.PLAIN, 13f)
			g.color = Palette.TEXT_MUTED
			g.drawString("${zone.percent}%", x + 20, rowY)
			g.color = Palette.TEXT
			g.drawString(zone.label, x + 58, rowY)

			val weightLabel = "${weightFmt.format(zone.weightKg)} кг"
			g.font = font(Font.BOLD, 13f)
			g.drawString(weightLabel, x + w - 20 - g.fontMetrics.stringWidth(weightLabel), rowY)

			val fillW = (barMaxW * zone.percent / 100.0).toInt().coerceAtLeast(8)
			g.color = Palette.ZONE_TRACK
			g.fillRoundRect(barX, rowY - 10, barMaxW, 8, 8, 8)
			g.color = zoneColor(zone.percent)
			g.fillRoundRect(barX, rowY - 10, fillW, 8, 8, 8)

			rowY += 34
		}
	}

	private fun drawFooter(
		g: Graphics2D,
		report: OneRepMaxEstimator.Report,
	) {
		val x = PADDING + 20
		val y = CANVAS_HEIGHT - PADDING - 52
		val w = CANVAS_WIDTH - (PADDING + 20) * 2
		g.color = Palette.FOOTER_BG
		g.fillRoundRect(x, y, w, 44, 16, 16)

		g.font = font(Font.PLAIN, 13f)
		g.color = Palette.TEXT
		val text =
			when {
				report.historicalBestKg == null ->
					"Первый расчёт 1ПМ по этому упражнению"
				report.historicalBestKg <= report.session.consensusKg + 0.01 ->
					"🏆 Рекорд 1ПМ по истории: ${weightFmt.format(report.historicalBestKg)} кг"
				else -> {
					val bestDate =
						report.historicalBestDate?.let { dateFmt.format(it) } ?: "—"
					"Рекорд по истории: ${weightFmt.format(report.historicalBestKg)} кг ($bestDate)"
				}
			}
		g.drawString(text, x + 18, y + 28)
	}

	private fun drawChip(
		g: Graphics2D,
		x: Int,
		y: Int,
		w: Int,
		h: Int,
		text: String,
		bg: Color,
		fg: Color,
	) {
		g.color = bg
		g.fill(RoundRectangle2D.Float(x.toFloat(), y.toFloat(), w.toFloat(), h.toFloat(), 14f, 14f))
		g.font = font(Font.BOLD, 12f)
		g.color = fg
		val textW = g.fontMetrics.stringWidth(text)
		g.drawString(text, x + (w - textW) / 2, y + 22)
	}

	private fun confidenceRu(confidence: OneRepMaxEstimator.Confidence): String =
		when (confidence) {
			OneRepMaxEstimator.Confidence.HIGH -> "высокая точность"
			OneRepMaxEstimator.Confidence.MEDIUM -> "средняя точность"
			OneRepMaxEstimator.Confidence.LOW -> "грубая оценка"
		}

	private fun zoneColor(percent: Int): Color =
		when {
			percent >= 90 -> Palette.ZONE_90
			percent >= 80 -> Palette.ZONE_80
			percent >= 70 -> Palette.ZONE_70
			else -> Palette.ZONE_60
		}

	private fun font(
		style: Int,
		size: Float,
	): Font {
		val candidates = listOf("DejaVu Sans", "SansSerif")
		for (name in candidates) {
			val base = Font(name, Font.PLAIN, 12)
			if (base.canDisplay('ж') && base.canDisplay('Я')) {
				return base.deriveFont(style, size)
			}
		}
		return Font(Font.SANS_SERIF, style, size.toInt())
	}

	private object Palette {
		val BG_TOP = Color(15, 23, 42)
		val BG_BOTTOM = Color(30, 41, 59)
		val CARD = Color(30, 37, 56)
		val HEADER_LEFT = Color(220, 38, 38)
		val HEADER_RIGHT = Color(249, 115, 22)
		val HEADER_BADGE = Color(254, 226, 226)
		val HEADER_SUBTITLE = Color(254, 215, 170)
		val HERO = Color(248, 250, 252)
		val TEXT = Color(226, 232, 240)
		val TEXT_MUTED = Color(148, 163, 184)
		val CHIP_BG = Color(51, 65, 85)
		val CHIP_TEXT = Color(226, 232, 240)
		val ZONE_PANEL = Color(15, 23, 42, 200)
		val ZONE_TRACK = Color(51, 65, 85)
		val ZONE_90 = Color(239, 68, 68)
		val ZONE_80 = Color(249, 115, 22)
		val ZONE_70 = Color(59, 130, 246)
		val ZONE_60 = Color(52, 211, 153)
		val FOOTER_BG = Color(15, 23, 42, 210)
	}

	companion object {
		private const val CANVAS_WIDTH = 1000
		private const val CANVAS_HEIGHT = 700
		private const val PADDING = 20
	}
}
