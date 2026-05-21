package dev.myutils.api

import org.springframework.boot.autoconfigure.SpringBootApplication
import org.springframework.boot.runApplication
import org.springframework.scheduling.annotation.EnableScheduling

@SpringBootApplication
@EnableScheduling
class MyUtilsApiApplication

fun main(args: Array<String>) {
	runApplication<MyUtilsApiApplication>(*args)
}
