package dev.myutils.api.infra.config

import org.springframework.context.annotation.Configuration
import org.springframework.scheduling.annotation.EnableAsync

@Configuration
@EnableAsync
@ConditionalOnTelegramBot
class AsyncConfig
