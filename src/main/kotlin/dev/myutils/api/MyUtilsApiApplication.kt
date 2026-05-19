package dev.myutils.api

import org.springframework.boot.autoconfigure.SpringBootApplication
import org.springframework.boot.runApplication

@SpringBootApplication
class MyUtilsApiApplication

fun main(args: Array<String>) {
	runApplication<MyUtilsApiApplication>(*args)
}
