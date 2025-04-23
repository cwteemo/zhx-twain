#ifndef UTILITIES_H
#define UTILITIES_H

#include <string>

// 流水号
std::string generateSerialNumber();
//使用文件名安全的流水号
std::string generateFilenameSafeSerialNumber();
#endif // UTILITIES_H