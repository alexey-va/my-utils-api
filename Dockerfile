# syntax=docker/dockerfile:1

# Gradle уже в образе — не качаем gradle-9.4.1-bin.zip через wrapper на каждый build.
FROM gradle:9.4.1-jdk21 AS build
WORKDIR /app

COPY build.gradle.kts settings.gradle.kts ./
RUN --mount=type=cache,target=/home/gradle/.gradle \
	gradle dependencies --no-daemon -q || true

ARG GIT_COMMIT=unknown
COPY src src
RUN --mount=type=cache,target=/home/gradle/.gradle \
	gradle bootJar --no-daemon -x test

FROM eclipse-temurin:21-jre
WORKDIR /app

RUN apt-get update \
	&& apt-get install -y --no-install-recommends curl \
	&& rm -rf /var/lib/apt/lists/*

RUN groupadd -r spring && useradd -r -g spring spring
USER spring:spring

COPY --from=build /app/build/libs/*.jar app.jar

EXPOSE 8080

HEALTHCHECK --interval=10s --timeout=5s --start-period=45s --retries=8 \
	CMD curl -fsS http://localhost:8080/api/health || exit 1

ENTRYPOINT ["java", "-jar", "app.jar"]
