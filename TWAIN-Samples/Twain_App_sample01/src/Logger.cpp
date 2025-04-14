#include "Logger.h"
#include <cstdarg>
#include <ctime>
#include <mutex>
#include <cstdio>

// 静态成员初始化
FILE* Logger::m_logFile = nullptr;
static std::mutex logMutex;

void Logger::Init() {
    std::lock_guard<std::mutex> lock(logMutex);
    if (!m_logFile) {
        fopen_s(&m_logFile, "twain.log", "a");
        if (m_logFile) {
            time_t now = time(nullptr);
            char timestamp[64];
            struct tm timeInfo;
            localtime_s(&timeInfo, &now);
            strftime(timestamp, sizeof(timestamp), "%Y-%m-%d %H:%M:%S", &timeInfo);
            fprintf(m_logFile, "\n=== Log Started at %s ===\n", timestamp);
            fflush(m_logFile);
        }
    }
}

void Logger::Cleanup() {
    std::lock_guard<std::mutex> lock(logMutex);
    if (m_logFile) {
        time_t now = time(nullptr);
        char timestamp[64];
        struct tm timeInfo;
        localtime_s(&timeInfo, &now);
        strftime(timestamp, sizeof(timestamp), "%Y-%m-%d %H:%M:%S", &timeInfo);
        fprintf(m_logFile, "\n=== Log Ended at %s ===\n", timestamp);
        fclose(m_logFile);
        m_logFile = nullptr;
    }
}

void Logger::Log(const char* format, ...) {
    std::lock_guard<std::mutex> lock(logMutex);
    if (!m_logFile) {
        Init();
        if (!m_logFile) return;
    }

    // 获取时间戳
    time_t now = time(nullptr);
    char timestamp[32];
    struct tm timeInfo;
    localtime_s(&timeInfo, &now);
    strftime(timestamp, sizeof(timestamp), "[%Y-%m-%d %H:%M:%S] ", &timeInfo);

    // 写入时间戳
    fprintf(m_logFile, "%s", timestamp);

    // 写入日志内容
    va_list args;
    va_start(args, format);
    vfprintf(m_logFile, format, args);
    va_end(args);

    // 确保换行
    if (format[strlen(format) - 1] != '\n') {
        fprintf(m_logFile, "\n");
    }

    // 立即刷新缓冲区
    fflush(m_logFile);
}

std::string Logger::GetCurrentTimeString() {
    time_t now = time(nullptr);
    char buffer[128];
    struct tm timeInfo;
    localtime_s(&timeInfo, &now);
    strftime(buffer, sizeof(buffer), "%Y-%m-%d %H:%M:%S", &timeInfo);
    return std::string(buffer);
}