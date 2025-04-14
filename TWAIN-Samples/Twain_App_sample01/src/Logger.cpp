#include "Logger.h"
#include <cstdarg>
#include <ctime>
#include <mutex>
#include <cstdio>
#include <Windows.h>
#include <direct.h>

// 静态成员初始化
FILE* Logger::m_logFile = nullptr;
std::string Logger::m_logFileName;
std::string Logger::m_logDirectory;
static std::once_flag initFlag;
static std::mutex logMutex;

const char* Logger::GetDefaultLogFileName() {
    return "twain.log";
}

std::string Logger::GetFullLogPath() {
    if (m_logDirectory.empty()) {
        return m_logFileName;
    }
    return m_logDirectory + "\\" + m_logFileName;
}

bool Logger::EnsureLogDirectory() {
    if (m_logDirectory.empty()) {
        return true;
    }

    // 尝试创建目录
    if (_mkdir(m_logDirectory.c_str()) == 0) {
        return true;
    }

    // 如果目录已存在，检查错误码
    if (errno == EEXIST) {
        return true;
    }

    // 其他错误
    char errorMsg[256];
    strerror_s(errorMsg, sizeof(errorMsg), errno);
    OutputDebugStringA(errorMsg);
    return false;
}

void Logger::SetLogDirectory(const char* logDir) {
    if (logDir) {
        m_logDirectory = logDir;
    } else {
        m_logDirectory.clear();
    }
}

void Logger::SetLogFileName(const char* logFileName) {
    std::lock_guard<std::mutex> lock(logMutex);
    if (logFileName) {
        m_logFileName = logFileName;
    } else {
        m_logFileName = GetDefaultLogFileName();
    }
}

void Logger::Init(const char* logFileName) {
    std::call_once(initFlag, [logFileName]() {
        try {
            // 设置日志文件名
            if (logFileName) {
                m_logFileName = logFileName;
            } else {
                m_logFileName = "twain.log";
            }
            
            if (!m_logFile) {
                // 确保日志目录存在
                if (!m_logDirectory.empty()) {
                    _mkdir(m_logDirectory.c_str());
                }

                // 获取完整的日志文件路径
                std::string fullPath = GetFullLogPath();
                
                errno_t err = fopen_s(&m_logFile, fullPath.c_str(), "a");
                if (err == 0 && m_logFile) {
                    time_t now = time(nullptr);
                    char timestamp[64];
                    struct tm timeInfo;
                    localtime_s(&timeInfo, &now);
                    strftime(timestamp, sizeof(timestamp), "%Y-%m-%d %H:%M:%S", &timeInfo);
                    fprintf(m_logFile, "\n=== Log Started at %s ===\n", timestamp);
                    fprintf(m_logFile, "Log file: %s\n", fullPath.c_str());
                    fflush(m_logFile);
                    OutputDebugStringA("Log file initialized successfully\n");
                }
            }
        }
        catch (const std::exception& e) {
            OutputDebugStringA(e.what());
        }
        catch (...) {
            OutputDebugStringA("Unknown exception in Logger::Init\n");
        }
    });
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
        return;
    }

    try {
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

        // 同时输出到调试窗口
        char buffer[1024];
        va_start(args, format);
        vsprintf_s(buffer, format, args);
        va_end(args);
        OutputDebugStringA(timestamp);
        OutputDebugStringA(buffer);
        if (buffer[strlen(buffer) - 1] != '\n') {
            OutputDebugStringA("\n");
        }
    }
    catch (const std::exception& e) {
        OutputDebugStringA(e.what());
    }
    catch (...) {
        OutputDebugStringA("Unknown exception in Logger::Log");
    }
}

std::string Logger::GetCurrentTimeString() {
    time_t now = time(nullptr);
    char buffer[128];
    struct tm timeInfo;
    localtime_s(&timeInfo, &now);
    strftime(buffer, sizeof(buffer), "%Y-%m-%d %H:%M:%S", &timeInfo);
    return std::string(buffer);
}