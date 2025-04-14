#include "http_server.h"
#include <afxwin.h>
#include <afxinet.h>
#include <afxsock.h>
#include <exception>
#include <string>
#include <map>
#include "../src/logger.h"

// 自定义消息定义
#define WM_HTTP_REQUEST (WM_USER + 100)
#define WM_GET_SCANNER_LIST (WM_USER + 101)

// 函数声明
std::string buildJSON(const std::map<std::string, std::string>& params, const std::string& url = "", const std::string& requestType = "");

// Declare 'lastResponse' as a CString
CString lastResponse;

// Initialize 'lastResponse' with a default value or fetch from a relevant source
// Example: lastResponse = GetLastResponse(); // Assuming GetLastResponse() is a function that retrieves the last response

// 声明辅助方法
void ParseHttpRequest(const std::string& request, std::string& url, std::string& params, 
                     std::map<std::string, std::string>& paramsMap, std::string& requestType);
void ParseUrlParameters(const std::string& fullURL, std::string& url, std::string& params, 
                        std::map<std::string, std::string>& paramsMap);
void ParseRequestBody(const std::string& request, const std::string& params, 
                     std::map<std::string, std::string>& paramsMap, std::string& requestType);
void ParseFormData(const std::string& request, const std::string& body, 
                  std::map<std::string, std::string>& paramsMap, std::string& requestType);
void ParseFormUrlEncoded(const std::string& body, 
                        std::map<std::string, std::string>& paramsMap, std::string& requestType);
void SendMessageToMainWindow(HWND hMainWnd, const std::map<std::string, std::string>& paramsMap, 
                            const std::string& url, const std::string& requestType, bool& messageSent);
std::string BuildHttpResponse(HttpServer* pHttpServer, HWND hMainWnd, bool scannerListRequested, 
                             bool containsScannersKeyword, const std::string& params, const std::string& request);
std::string GetScannerListResponse(HttpServer* pHttpServer, HWND hMainWnd);

bool HttpServer::Start(int port) {
    try {
        // 保存端口号
        m_port = port;
        
        // 初始化WSA
        WSADATA wsaData;
        int result = WSAStartup(MAKEWORD(2, 2), &wsaData);
        if (result != 0) {
            Logger::Log("WSA initialization failed: %d", result);
            return false;
        }
        m_wsaInitialized = true;
        Logger::Log("WSA initialized successfully");
        
        // 创建socket
        m_serverSocket = socket(AF_INET, SOCK_STREAM, IPPROTO_TCP);
        if (m_serverSocket == INVALID_SOCKET) {
            Logger::Log("Socket creation failed: %d", WSAGetLastError());
            return false;
        }
        
        // 绑定IP和端口
        sockaddr_in serverAddr;
        serverAddr.sin_family = AF_INET;
        serverAddr.sin_addr.s_addr = htonl(INADDR_ANY);
        serverAddr.sin_port = htons(port);
        result = bind(m_serverSocket, (sockaddr*)&serverAddr, sizeof(serverAddr));
        if (result == SOCKET_ERROR) {
            Logger::Log("Bind failed: %d", WSAGetLastError());
            closesocket(m_serverSocket);
            return false;
        }
        
        // 监听
        result = listen(m_serverSocket, SOMAXCONN);
        if (result == SOCKET_ERROR) {
            Logger::Log("Listen failed: %d", WSAGetLastError());
            closesocket(m_serverSocket);
            return false;
        }
        
        // 检查并记录主窗口句柄状态
        HWND mainWnd = GetMainWindow();
        if (mainWnd && ::IsWindow(mainWnd)) {
            Logger::Log("Starting server with valid main window handle: %p", mainWnd);
        } else {
            Logger::Log("WARNING: Starting server with invalid main window handle: %p", mainWnd);
            
            // 尝试从应用程序获取主窗口句柄
            CWnd* pMainWnd = AfxGetApp()->GetMainWnd();
            if (pMainWnd && ::IsWindow(pMainWnd->GetSafeHwnd())) {
                SetMainWindow(pMainWnd->GetSafeHwnd());
                Logger::Log("Retrieved and set main window handle from app: %p", pMainWnd->GetSafeHwnd());
            }
        }
        
        // 设置运行标志
        m_isRunning = true;
        m_running = true;
        
        // 创建服务器线程
        m_pThread = AfxBeginThread(ServerThread, this);
        
        if (!m_pThread) {
            Logger::Log("Failed to create server thread");
            m_isRunning = false;
            m_running = false;
            closesocket(m_serverSocket);
            return false;
        }
        
        return true;
    } catch (std::exception& e) {
        Logger::Log("Exception in Start method: %s", e.what());
    } catch (...) {
        Logger::Log("Unknown exception in Start method");
    }
    
    return false;
}

void HttpServer::Stop() {
    if (!m_isRunning) {
        return;
    }

    // 首先标记停止状态
    m_isRunning = false;
    
    Logger::Log("HTTP server stopping...");
    
    // 关闭套接字 - 这会导致accept调用返回错误，使线程能够退出循环
    if (m_serverSocket != INVALID_SOCKET) {
        Logger::Log("Closing server socket...");
        shutdown(m_serverSocket, SD_BOTH);
        closesocket(m_serverSocket);
        m_serverSocket = INVALID_SOCKET;
    }

    // 等待线程结束，设置合理的超时时间
    if (m_pThread) {
        Logger::Log("Waiting for server thread to terminate...");
        // 使用1秒超时，而不是无限等待，避免可能的死锁
        DWORD result = WaitForSingleObject(m_pThread->m_hThread, 1000);
        if (result == WAIT_TIMEOUT) {
            Logger::Log("Warning: Server thread did not terminate within timeout");
        }
        m_pThread = nullptr;
    }

    // 清理WSA，如果已初始化
    if (m_wsaInitialized) {
        Logger::Log("WSA cleanup in progress");
        WSACleanup();
        m_wsaInitialized = false;
        Logger::Log("WSA cleanup completed");
    }

    Logger::Log("HTTP server stopped");
}

UINT HttpServer::ServerThread(LPVOID pParam) {
    HttpServer* pServer = (HttpServer*)pParam;
    pServer->RunServer();
    return 0;
}

void HttpServer::RunServer() {
    // 使用成员函数获取主窗口句柄，避免直接使用成员变量
    HWND mainWnd = GetMainWindow();
    Logger::Log("RunServer: server thread started, main window handle: %p", mainWnd);
    
    while (m_isRunning) {
        SOCKET clientSocket = accept(m_serverSocket, nullptr, nullptr);
        if (clientSocket != INVALID_SOCKET) {
            // 每次都获取当前最新的主窗口句柄，而不是使用启动时的句柄
            HWND currentMainWnd = GetMainWindow();
            
            // 记录并验证主窗口句柄是否有效
            if (currentMainWnd && ::IsWindow(currentMainWnd)) {
                Logger::Log("RunServer: valid main window handle: %p", currentMainWnd);
            } else {
                Logger::Log("WARNING: main window handle (%p) is invalid or NULL", currentMainWnd);
                
                // 尝试从应用程序获取主窗口句柄
                CWnd* pMainWnd = AfxGetApp()->GetMainWnd();
                if (pMainWnd && ::IsWindow(pMainWnd->GetSafeHwnd())) {
                    currentMainWnd = pMainWnd->GetSafeHwnd();
                    Logger::Log("RunServer: retrieved main window handle from app: %p", currentMainWnd);
                    
                    // 同时更新HttpServer实例中的主窗口句柄
                    SetMainWindow(currentMainWnd);
                }
            }
            
            // 创建包含自身指针、主窗口句柄和socket的数据结构
            struct RequestData {
                HttpServer* pServer;
                HWND hMainWnd;
                SOCKET clientSocket;
            };
            
            auto* pData = new RequestData{this, currentMainWnd, clientSocket};

            Logger::Log("RunServer: create client request thread, use main window handle: %p", currentMainWnd);
            AfxBeginThread(&HttpServer::ProcessRequest, (LPVOID)pData);
        }
        else {
            if (m_isRunning) {  // 只在服务器仍在运行时报告错误
                Logger::Log("Accept failed. Error: %d", WSAGetLastError());
            }
        }
        Sleep(100); // 避免CPU占用过高
    }
}

// 修改ProcessRequest为静态函数，以便作为线程函数使用
UINT HttpServer::ProcessRequest(LPVOID pParam) {
    // 添加异常处理，确保所有异常都被捕获
    try {
        // 检查参数是否有效
        if (!pParam) {
            Logger::Log("ProcessRequest: Invalid parameter (NULL)");
            return 1;
        }

        // 使用新的RequestData结构
        struct RequestData {
            HttpServer* pServer;
            HWND hMainWnd;
            SOCKET clientSocket;
        };
        
        // 获取HttpServer实例、主窗口句柄和客户端socket
        auto* pData = (RequestData*)pParam;
        HttpServer* pHttpServer = pData->pServer;
        HWND hMainWnd = pData->hMainWnd;
        SOCKET clientSocket = pData->clientSocket;
        
        Logger::Log("ProcessRequest: Received parameters - HttpServer: %p, main window handle: %p, socket: %d", 
                   pHttpServer, hMainWnd, clientSocket);
        
        delete pData;  // 释放内存
        pData = nullptr;  // 防止悬挂指针
        
        // 验证客户端socket
        if (clientSocket == INVALID_SOCKET) {
            Logger::Log("ProcessRequest: Invalid client socket");
            return 1;
        }
        
        char recvBuffer[1024];
        int recvLen = recv(clientSocket, recvBuffer, sizeof(recvBuffer) - 1, 0);
        if (recvLen > 0) {
            recvBuffer[recvLen] = '\0';
            std::string request(recvBuffer);
            
            Logger::Log("Received HTTP request: %s", request.c_str());
            
            // 解析HTTP请求
            std::string url = "/";
            std::string params = "";
            std::map<std::string, std::string> paramsMap;
            std::string requestType = "";
            
            // 解析HTTP请求，提取URL、参数和请求类型
            ParseHttpRequest(request, url, params, paramsMap, requestType);
            
            bool messageSent = false;
            
            // 发送消息到主窗口
            if (hMainWnd && ::IsWindow(hMainWnd)) {
                SendMessageToMainWindow(hMainWnd, paramsMap, url, requestType, messageSent);
            } else {
                Logger::Log("No valid main window handle available (handle: %p, IsWindow: %d)", 
                        hMainWnd, hMainWnd ? ::IsWindow(hMainWnd) : 0);
            }
            
            // 检查请求是否是扫描仪列表请求
            bool scannerListRequested = (requestType == "ScannerList");
            Logger::Log("Scanner list requested: %s", scannerListRequested ? "YES" : "NO");

            // 检查参数中是否包含handle=scanners
            if (!scannerListRequested && paramsMap.find("handle") != paramsMap.end() && paramsMap["handle"] == "scanners") {
                scannerListRequested = true;
                requestType = "ScannerList";
                Logger::Log("Scanner list explicitly requested via params");
            }

            // 检查请求是否包含scanners关键词但不是扫描仪列表请求
            bool containsScannersKeyword = (!scannerListRequested && request.find("scanners") != std::string::npos);
            Logger::Log("Contains 'scanners' keyword: %s", containsScannersKeyword ? "YES" : "NO");
            
            // 构建并发送HTTP响应
            std::string response = BuildHttpResponse(pHttpServer, hMainWnd, scannerListRequested, 
                                                   containsScannersKeyword, params, request);
            
            send(clientSocket, response.c_str(), response.length(), 0);
            Logger::Log("Response sent to client");
        } else if (recvLen == 0) {
            Logger::Log("Client closed connection before sending data");
        } else {
            Logger::Log("Error receiving data: %d", WSAGetLastError());
        }
        
        // 关闭客户端socket
        if (clientSocket != INVALID_SOCKET) {
            shutdown(clientSocket, SD_BOTH);  // 确保完全关闭
            closesocket(clientSocket);
            clientSocket = INVALID_SOCKET;
        }
        
        return 0;
    } catch (std::exception& e) {
        Logger::Log("Exception in ProcessRequest: %s", e.what());
    } catch (...) {
        Logger::Log("Unknown exception in ProcessRequest");
    }
    
    return 1;  // 发生错误时返回非零值
}

void HttpServer::SendResponse(SOCKET clientSocket, const std::string& response)
{
    send(clientSocket, response.c_str(), response.length(), 0);
}

void HttpServer::PostMessageToMain(UINT message, WPARAM wParam, LPARAM lParam)
{
    HWND hMainWnd = GetMainWindow();
    if (hMainWnd && ::IsWindow(hMainWnd))
    {
        ::PostMessage(hMainWnd, message, wParam, lParam);
    }
}

// 简单JSON构建函数
std::string buildJSON(const std::map<std::string, std::string>& params, const std::string& url, const std::string& requestType) {
    std::string json = "{";
    
    // 添加URL字段
    if (!url.empty()) {
        json += "\"url\":\"" + url + "\",";
    }
    
    // 添加requestType字段
    if (!requestType.empty()) {
        json += "\"requestType\":\"" + requestType + "\",";
    }
    
    // 添加params对象
    json += "\"params\":{";
    bool first = true;
    for (const auto& param : params) {
        if (!first) {
            json += ",";
        }
        json += "\"" + param.first + "\":\"" + param.second + "\"";
        first = false;
    }
    json += "}}";
    
    return json;
}

// 实现辅助方法
void ParseHttpRequest(const std::string& request, std::string& url, std::string& params, 
                     std::map<std::string, std::string>& paramsMap, std::string& requestType) {
    Logger::Log("Parsing HTTP request");
    
    // 检查HTTP请求类型
    if (request.find("GET") == 0) {
        requestType = "GET";
    } else if (request.find("POST") == 0) {
        requestType = "POST";
    } else {
        // 默认为GET
        requestType = "GET";
    }
    
    Logger::Log("Request type: %s", requestType.c_str());
    
    // 解析URL和参数
    size_t urlStart = request.find(' ') + 1;
    size_t urlEnd = request.find(' ', urlStart);
    
    if (urlStart != std::string::npos && urlEnd != std::string::npos) {
        std::string fullURL = request.substr(urlStart, urlEnd - urlStart);
        
        // 解析URL参数
        ParseUrlParameters(fullURL, url, params, paramsMap);
        
        // 如果是POST请求，还需要解析请求体
        if (requestType == "POST") {
            ParseRequestBody(request, params, paramsMap, requestType);
        }
        
        // 特殊处理：检查是否是扫描仪列表请求
        if ((url == "/scanners" || url == "/scanners/") || 
            (paramsMap.find("handle") != paramsMap.end() && paramsMap["handle"] == "scanners")) {
            requestType = "ScannerList";
        }
    }
    
    Logger::Log("Parsed URL: %s", url.c_str());
    Logger::Log("Parsed params: %s", params.c_str());
}

void ParseUrlParameters(const std::string& fullURL, std::string& url, std::string& params, 
                        std::map<std::string, std::string>& paramsMap) {
    Logger::Log("Parsing URL parameters from: %s", fullURL.c_str());
    
    // 查找参数分隔符'?'
    size_t paramPos = fullURL.find('?');
    
    if (paramPos != std::string::npos) {
        // 有参数
        url = fullURL.substr(0, paramPos);
        params = fullURL.substr(paramPos + 1);
        
        // 解析参数
        size_t pos = 0;
        std::string param;
        while ((pos = params.find('&')) != std::string::npos || !params.empty()) {
            param = (pos == std::string::npos) ? params : params.substr(0, pos);
            
            size_t equalPos = param.find('=');
            if (equalPos != std::string::npos) {
                std::string key = param.substr(0, equalPos);
                std::string value = param.substr(equalPos + 1);
                paramsMap[key] = value;
                Logger::Log("Parsed parameter: %s = %s", key.c_str(), value.c_str());
            }
            
            if (pos == std::string::npos) {
                break;
            }
            params = params.substr(pos + 1);
        }
    } else {
        // 没有参数
        url = fullURL;
        params = "";
    }
}

void ParseRequestBody(const std::string& request, const std::string& params, 
                     std::map<std::string, std::string>& paramsMap, std::string& requestType) {
    Logger::Log("Parsing request body");
    
    // 查找请求头结束标记
    size_t bodyStart = request.find("\r\n\r\n");
    if (bodyStart == std::string::npos) {
        Logger::Log("No request body found");
        return;
    }
    
    bodyStart += 4; // 跳过"\r\n\r\n"
    std::string body = request.substr(bodyStart);
    
    // 检查Content-Type
    size_t contentTypePos = request.find("Content-Type:");
    if (contentTypePos != std::string::npos) {
        size_t contentTypeEnd = request.find("\r\n", contentTypePos);
        std::string contentType = request.substr(contentTypePos + 14, contentTypeEnd - (contentTypePos + 14));
        
        // 去除前后空格
        contentType.erase(0, contentType.find_first_not_of(" \t"));
        contentType.erase(contentType.find_last_not_of(" \t") + 1);
        
        Logger::Log("Content-Type: %s", contentType.c_str());
        
        if (contentType.find("multipart/form-data") != std::string::npos) {
            // 处理multipart/form-data
            ParseFormData(request, body, paramsMap, requestType);
        } else if (contentType.find("application/x-www-form-urlencoded") != std::string::npos) {
            // 处理application/x-www-form-urlencoded
            ParseFormUrlEncoded(body, paramsMap, requestType);
        } else if (contentType.find("application/json") != std::string::npos) {
            // 处理JSON请求
            // 这里简化处理，假设为JSON格式
            requestType = "JSON";
            
            // 实际项目中应该使用JSON解析库，这里简化处理
            Logger::Log("JSON body found: %s", body.c_str());
            
            // 假设JSON格式为 {"key1":"value1","key2":"value2"}
            // 简单解析，无法处理嵌套JSON，实际项目中应使用JSON库
            size_t pos = 0;
            while ((pos = body.find("\":\"", pos)) != std::string::npos) {
                // 向前查找键名开始位置
                size_t keyStart = body.rfind("\"", pos - 1);
                if (keyStart == std::string::npos) continue;
                
                std::string key = body.substr(keyStart + 1, pos - keyStart - 1);
                
                // 向后查找值结束位置
                size_t valueStart = pos + 3;
                size_t valueEnd = body.find("\"", valueStart);
                if (valueEnd == std::string::npos) continue;
                
                std::string value = body.substr(valueStart, valueEnd - valueStart);
                
                paramsMap[key] = value;
                Logger::Log("Parsed JSON parameter: %s = %s", key.c_str(), value.c_str());
                
                pos = valueEnd + 1;
            }
        }
    }
}

void ParseFormData(const std::string& request, const std::string& body, 
                  std::map<std::string, std::string>& paramsMap, std::string& requestType) {
    Logger::Log("Parsing multipart/form-data");
    
    // 获取boundary
    size_t boundaryPos = request.find("boundary=");
    if (boundaryPos == std::string::npos) {
        Logger::Log("No boundary found in multipart/form-data");
        return;
    }
    
    size_t boundaryEnd = request.find("\r\n", boundaryPos);
    std::string boundary = request.substr(boundaryPos + 9, boundaryEnd - (boundaryPos + 9));
    
    // 去除引号（如果有）
    if (!boundary.empty() && boundary.front() == '"' && boundary.back() == '"') {
        boundary = boundary.substr(1, boundary.length() - 2);
    }
    
    Logger::Log("Found boundary: %s", boundary.c_str());
    
    // 完整的boundary格式为 --boundary\r\n
    std::string fullBoundary = "--" + boundary + "\r\n";
    std::string endBoundary = "--" + boundary + "--";
    
    // 解析多部分数据
    size_t pos = 0;
    while ((pos = body.find(fullBoundary, pos)) != std::string::npos) {
        pos += fullBoundary.length();
        
        // 查找表单项头部结束位置
        size_t headerEnd = body.find("\r\n\r\n", pos);
        if (headerEnd == std::string::npos) break;
        
        // 解析表单项头部
        std::string header = body.substr(pos, headerEnd - pos);
        
        // 查找名称
        size_t namePos = header.find("name=\"");
        if (namePos == std::string::npos) continue;
        
        size_t nameEnd = header.find("\"", namePos + 6);
        if (nameEnd == std::string::npos) continue;
        
        std::string name = header.substr(namePos + 6, nameEnd - (namePos + 6));
        
        // 查找表单项内容
        size_t contentStart = headerEnd + 4;
        size_t contentEnd = body.find("\r\n--" + boundary, contentStart);
        if (contentEnd == std::string::npos) break;
        
        std::string content = body.substr(contentStart, contentEnd - contentStart);
        
        // 存储表单项
        paramsMap[name] = content;
        Logger::Log("Parsed form-data: %s = %s", name.c_str(), content.c_str());
        
        pos = contentEnd;
    }
}

void ParseFormUrlEncoded(const std::string& body, 
                        std::map<std::string, std::string>& paramsMap, std::string& requestType) {
    Logger::Log("Parsing application/x-www-form-urlencoded");
    
    // 解析类似URL参数的格式：key1=value1&key2=value2
    std::string params = body;
    size_t pos = 0;
    std::string param;
    
    while ((pos = params.find('&')) != std::string::npos || !params.empty()) {
        param = (pos == std::string::npos) ? params : params.substr(0, pos);
        
        size_t equalPos = param.find('=');
        if (equalPos != std::string::npos) {
            std::string key = param.substr(0, equalPos);
            std::string value = param.substr(equalPos + 1);
            
            // URL解码（简化版）
            // TODO: 实现完整的URL解码
            
            paramsMap[key] = value;
            Logger::Log("Parsed form parameter: %s = %s", key.c_str(), value.c_str());
        }
        
        if (pos == std::string::npos) {
            break;
        }
        params = params.substr(pos + 1);
    }
}

void SendMessageToMainWindow(HWND hMainWnd, const std::map<std::string, std::string>& paramsMap, 
                            const std::string& url, const std::string& requestType, bool& messageSent) {
    Logger::Log("Sending message to main window");
    
    if (!hMainWnd || !::IsWindow(hMainWnd)) {
        Logger::Log("Invalid main window handle: %p", hMainWnd);
        messageSent = false;
        return;
    }
    
    // 构建JSON参数
    std::string jsonParams = buildJSON(paramsMap, url, requestType);
    Logger::Log("Built JSON params: %s", jsonParams.c_str());
    
    // 分配内存并复制JSON数据
    HGLOBAL hMem = ::GlobalAlloc(GMEM_MOVEABLE, jsonParams.length() + 1);
    if (!hMem) {
        Logger::Log("Failed to allocate memory for params");
        messageSent = false;
        return;
    }
    
    char* pData = (char*)::GlobalLock(hMem);
    if (!pData) {
        ::GlobalFree(hMem);
        Logger::Log("Failed to lock memory");
        messageSent = false;
        return;
    }
    
    strcpy_s(pData, jsonParams.length() + 1, jsonParams.c_str());
    ::GlobalUnlock(hMem);
    
    // 使用PostMessage异步发送消息
    BOOL result = ::PostMessage(hMainWnd, WM_HTTP_REQUEST, (WPARAM)hMem, 0);
    
    if (result) {
        Logger::Log("Message sent to main window successfully");
        messageSent = true;
    } else {
        Logger::Log("Failed to send message to main window: %d", GetLastError());
        ::GlobalFree(hMem);
        messageSent = false;
    }
}

std::string BuildHttpResponse(HttpServer* pHttpServer, HWND hMainWnd, bool scannerListRequested, 
                             bool containsScannersKeyword, const std::string& params, const std::string& request) {
    Logger::Log("Building HTTP response");
    
    std::string response;
    
    // 添加HTTP响应头
    response = "HTTP/1.1 200 OK\r\n";
    response += "Content-Type: application/json\r\n";
    response += "Access-Control-Allow-Origin: *\r\n";  // 允许跨域请求
    response += "Connection: close\r\n";
    response += "\r\n";
    
    // 构建响应内容
    if (scannerListRequested) {
        // 获取扫描仪列表
        std::string scannerList = GetScannerListResponse(pHttpServer, hMainWnd);
        response += scannerList;
    } else if (containsScannersKeyword) {
        // 尝试通过备用方式获取扫描仪列表
        std::string scannerList = GetScannerListResponse(pHttpServer, hMainWnd);
        response += scannerList;
    } else {
        // 默认响应
        response += "{\"errorCode\":0,\"msg\":\"Request processed\",\"data\":{}}";
    }
    
    Logger::Log("Response built: %s", response.c_str());
    return response;
}

std::string GetScannerListResponse(HttpServer* pHttpServer, HWND hMainWnd) {
    Logger::Log("Getting scanner list response");
    
    // 先检查是否有缓存的扫描仪列表（全局变量lastResponse）
    if (!lastResponse.IsEmpty()) {
        Logger::Log("Using cached scanner list");
        
        try {
            // 获取缓存的扫描仪列表字符串
            std::string cachedResponse;
#ifdef _UNICODE
            // Unicode版本 - 将CString (wchar_t*) 转换为UTF-8的std::string
            // 使用安全的方式进行字符转换
            CStringA cstrA(lastResponse);
            cachedResponse = cstrA.GetString();
#else
            // ANSI版本 - 直接从CString获取char*
            cachedResponse = lastResponse.GetString();
#endif
            
            // 检查缓存的响应格式，如果已经是新格式则直接返回
            if (cachedResponse.find("\"errorCode\"") != std::string::npos) {
                return cachedResponse;
            }
            
            // 如果是旧格式的JSON字符串，需要转换为新格式
            // 提取scanners数组部分
            size_t scannersPos = cachedResponse.find("\"scanners\"");
            if (scannersPos != std::string::npos) {
                size_t arrayStart = cachedResponse.find("[", scannersPos);
                size_t arrayEnd = cachedResponse.find_last_of("]");
                
                if (arrayStart != std::string::npos && arrayEnd != std::string::npos && arrayStart < arrayEnd) {
                    std::string scannersArray = cachedResponse.substr(arrayStart, arrayEnd - arrayStart + 1);
                    return "{\"errorCode\":0,\"msg\":\"Scanner list retrieved\",\"data\":{\"scanners\":" + scannersArray + "}}";
                }
            }
            
            // 如果无法解析，返回一个基本的成功响应
            return "{\"errorCode\":0,\"msg\":\"Scanner list retrieved\",\"data\":{\"scanners\":[]}}";
        }
        catch (std::exception& e) {
            Logger::Log("Exception during string conversion: %s", e.what());
            return "{\"errorCode\":1,\"msg\":\"String conversion failed\",\"data\":{}}";
        }
        catch (...) {
            Logger::Log("Unknown exception during string conversion");
            return "{\"errorCode\":1,\"msg\":\"Unknown string conversion error\",\"data\":{}}";
        }
    }
    
    // 构建一个固定的JSON响应作为备用方案
    std::string scannersArray = "[";
    
    // 从主窗口获取扫描仪列表可能导致死锁，我们使用一种非阻塞的方式
    if (hMainWnd && ::IsWindow(hMainWnd)) {
        Logger::Log("Attempting to get scanner list from main window - using async approach");
        
        // 首先尝试从应用程序中直接获取扫描仪信息
        try {
            if (pHttpServer && AfxGetApp()) {
                CString backupScannerList;
                // 从应用程序配置文件中获取备份的扫描仪列表
                backupScannerList = AfxGetApp()->GetProfileString(_T("Settings"), _T("LastScannerList"), _T(""));
                
                if (!backupScannerList.IsEmpty()) {
                    // 使用安全方式转换字符串
                    CStringA scannerListA(backupScannerList);
                    std::string scannerListStr = scannerListA.GetString();
                    
                    Logger::Log("Using backup scanner list from app profile: %s", scannerListStr.c_str());
                    
                    // 检查前缀
                    if (scannerListStr.find("SCANNERS:") == 0) {
                        scannerListStr = scannerListStr.substr(9); // 去掉 "SCANNERS:" 前缀
                        
                        // 将旧格式转换为JSON格式
                        bool first = true;
                        size_t pos = 0;
                        size_t nextPos = 0;
                        
                        while ((nextPos = scannerListStr.find(";", pos)) != std::string::npos) {
                            if (nextPos > pos) {  // 确保我们有有效的字符串
                                std::string scanner = scannerListStr.substr(pos, nextPos - pos);
                                
                                // 提取扫描仪名称，格式为 "[index]name"
                                size_t nameStart = scanner.find("]");
                                if (nameStart != std::string::npos && nameStart + 1 < scanner.length()) {
                                    std::string scannerName = scanner.substr(nameStart + 1);
                                    
                                    if (!first) {
                                        scannersArray += ",";
                                    }
                                    
                                    scannersArray += "{\"name\":\"" + scannerName + "\"}";
                                    first = false;
                                }
                            }
                            
                            pos = nextPos + 1;
                            if (pos >= scannerListStr.length()) {
                                break;  // 避免越界
                            }
                        }
                    }
                }
            }
        }
        catch (std::exception& e) {
            Logger::Log("Exception in scanner list processing: %s", e.what());
            return "{\"errorCode\":1,\"msg\":\"Exception in scanner list processing\",\"data\":{}}";
        }
        catch (...) {
            Logger::Log("Unknown exception in scanner list processing");
            return "{\"errorCode\":1,\"msg\":\"Unknown error in scanner list processing\",\"data\":{}}";
        }
    } else {
        Logger::Log("Main window handle invalid, cannot get scanner list");
    }
    
    // 完成JSON响应
    scannersArray += "]";
    
    // 构建新格式的响应
    return "{\"errorCode\":0,\"msg\":\"Scanner list retrieved\",\"data\":{\"scanners\":" + scannersArray + "}}";
}
