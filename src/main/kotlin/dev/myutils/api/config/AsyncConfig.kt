package dev.myutils.api.config

import org.springframework.context.annotation.Configuration
import org.springframework.scheduling.annotation.EnableAsync

@Configuration
@EnableAsync
@ConditionalOnTelegramBot
class AsyncConfig
