#include "utilities.h"
#include <time.h>
#include <stdio.h>
#include <stdlib.h>

// 修改为以下代码，生成一个基于时间戳的流水号
std::string generateSerialNumber() {
    time_t now = time(NULL);
    struct tm timeinfo;
    char buffer[30];
    
    // 获取本地时间
#if defined(_WIN32) || defined(_WIN64)
    localtime_s(&timeinfo, &now);
    strftime(buffer, sizeof(buffer), "%Y%m%d_%H%M%S", &timeinfo);
#else
    timeinfo = *localtime(&now);
    strftime(buffer, sizeof(buffer), "%Y%m%d_%H%M%S", &timeinfo);
#endif
    
    // 添加随机数，确保唯一性
    srand((unsigned int)time(NULL));
    int randomPart = rand() % 1000; // 0-999的随机数
    
    char fullSerial[50];
    sprintf_s(fullSerial, sizeof(fullSerial), "SCAN_%s_%03d", buffer, randomPart);
    
    return std::string(fullSerial);
}

std::string generateFilenameSafeSerialNumber() {
    std::string serial = generateSerialNumber();
    
    // 替换文件名中不允许的字符
    for (size_t i = 0; i < serial.length(); i++) {
        char c = serial[i];
        if (c == '/' || c == '\\' || c == ':' || c == '*' || 
            c == '?' || c == '"' || c == '<' || c == '>' || c == '|') {
            serial[i] = '_';
        }
    }
    
    return serial;
}