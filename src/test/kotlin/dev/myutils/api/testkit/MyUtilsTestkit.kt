package dev.myutils.api.testkit

import dev.myutils.api.infra.config.Environment
import dev.myutils.api.testkit.impl.TestingClientsConfiguration
import org.springframework.boot.test.context.SpringBootTest
import org.springframework.context.annotation.Import
import org.springframework.context.annotation.ImportSelector
import org.springframework.core.type.AnnotationMetadata
import org.springframework.test.context.ActiveProfiles
import org.springframework.test.context.ActiveProfilesResolver
import java.lang.annotation.Inherited

// --- annotation + Spring test bootstrap ---

@Target(AnnotationTarget.CLASS)
@Retention(AnnotationRetention.RUNTIME)
@Inherited
@SpringBootTest
@ActiveProfiles(resolver = EnvironmentActiveProfilesResolver::class)
@Import(EnvironmentImportSelector::class)
annotation class MyUtilsSpringTest(
	val environment: Environment = Environment.PRODUCTION,
)

class EnvironmentActiveProfilesResolver : ActiveProfilesResolver {
	override fun resolve(testClass: Class<*>): Array<String> {
		val annotated = findMyUtilsSpringTest(testClass)?.environment
		return Environment.resolve(annotated).testSpringProfiles()
	}

	private fun findMyUtilsSpringTest(testClass: Class<*>): MyUtilsSpringTest? {
		var clazz: Class<*>? = testClass
		while (clazz != null) {
			clazz.getAnnotation(MyUtilsSpringTest::class.java)?.let { return it }
			clazz = clazz.superclass
		}
		return null
	}
}

class EnvironmentImportSelector : ImportSelector {
	override fun selectImports(importingClassMetadata: AnnotationMetadata): Array<String> {
		val testClass = Class.forName(importingClassMetadata.className)
		val environment = Environment.resolve(findEnvironment(testClass))
		return if (environment.usesFakeClients) {
			arrayOf(TestingClientsConfiguration::class.java.name)
		} else {
			emptyArray()
		}
	}

	private fun findEnvironment(testClass: Class<*>): Environment? {
		var clazz: Class<*>? = testClass
		while (clazz != null) {
			clazz.getAnnotation(MyUtilsSpringTest::class.java)?.let { return it.environment }
			clazz = clazz.superclass
		}
		return null
	}
}

internal fun Environment.testSpringProfiles(): Array<String> =
	when (this) {
		Environment.PRODUCTION -> arrayOf("test")
		Environment.TESTING -> arrayOf("test", Environment.SPRING_PROFILE)
	}

// --- base classes ---

/** Requires Postgres + Redis — run `docker compose up -d` before tests. */
@MyUtilsSpringTest(Environment.PRODUCTION)
abstract class IntegrationTestBase

/** [Environment.TESTING]: real infra, outbound clients overridden via @Primary fakes. */
@MyUtilsSpringTest(Environment.TESTING)
abstract class TestingIntegrationTestBase : IntegrationTestBase()
