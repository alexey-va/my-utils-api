package dev.myutils.api.domain

import org.springframework.data.jpa.repository.JpaRepository

interface AppSettingRepository : JpaRepository<AppSetting, String>
