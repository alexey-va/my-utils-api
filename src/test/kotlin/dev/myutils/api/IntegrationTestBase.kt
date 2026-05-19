package dev.myutils.api

import org.springframework.boot.test.context.SpringBootTest
import org.springframework.test.context.ActiveProfiles

/** Requires Postgres + Redis — run `docker compose up -d` before tests. */
@SpringBootTest
@ActiveProfiles("test")
abstract class IntegrationTestBase
