/***************************************************************************
 * HTTP服务器实现（纯Win32实现，不依赖MFC）
 * 提供RESTful API接口，使任何语言都可以通过HTTP调用TWAIN功能
 * 
 * 参考：https://github.com/twain/twain-samples
 ***************************************************************************/

#ifdef TWH_CMP_MSC

#include "http_server.h"
#include "TwainAppCMD.h"
#include "Logger.h"
#include "main.h"
#include <sstream>
#include <algorithm>
#include <iomanip>

// 全局HTTP服务器实例
HttpServer* g_pHttpServer = nullptr;

//////////////////////////////////////////////////////////////////////////////
// HttpServer 实现
//////////////////////////////////////////////////////////////////////////////

HttpServer::HttpServer()
    : m_serverSocket(INVALID_SOCKET)
    , m_isRunning(false)
    , m_port(8080)
    , m_threadHandle(NULL)
    , m_wsaInitialized(false)
{
}

HttpServer::~HttpServer()
{
    Stop();
}

bool HttpServer::Start(int port)
{
    if (m_isRunning.load()) {
        Logger::Log("HTTP Server: Already running on port %d", m_port);
        return false;
    }

    m_port = port;

    // 初始化WSA
    WSADATA wsaData;
    int result = WSAStartup(MAKEWORD(2, 2), &wsaData);
    if (result != 0) {
        Logger::Log("HTTP Server: WSAStartup failed: %d", result);
        return false;
    }
    m_wsaInitialized = true;

    // 创建socket
    m_serverSocket = socket(AF_INET, SOCK_STREAM, IPPROTO_TCP);
    if (m_serverSocket == INVALID_SOCKET) {
        Logger::Log("HTTP Server: Socket creation failed: %d", WSAGetLastError());
        WSACleanup();
        m_wsaInitialized = false;
        return false;
    }

    // 设置socket选项（允许地址重用）
    int opt = 1;
    setsockopt(m_serverSocket, SOL_SOCKET, SO_REUSEADDR, (char*)&opt, sizeof(opt));

    // 绑定地址和端口
    sockaddr_in serverAddr;
    serverAddr.sin_family = AF_INET;
    serverAddr.sin_addr.s_addr = htonl(INADDR_ANY);
    serverAddr.sin_port = htons(port);

    result = bind(m_serverSocket, (sockaddr*)&serverAddr, sizeof(serverAddr));
    if (result == SOCKET_ERROR) {
        Logger::Log("HTTP Server: Bind failed: %d", WSAGetLastError());
        closesocket(m_serverSocket);
        WSACleanup();
        m_wsaInitialized = false;
        return false;
    }

    // 监听
    result = listen(m_serverSocket, SOMAXCONN);
    if (result == SOCKET_ERROR) {
        Logger::Log("HTTP Server: Listen failed: %d", WSAGetLastError());
        closesocket(m_serverSocket);
        WSACleanup();
        m_wsaInitialized = false;
        return false;
    }

    // 启动服务器线程
    m_isRunning = true;
    m_threadHandle = CreateThread(NULL, 0, ServerThreadProc, this, 0, NULL);
    if (m_threadHandle == NULL) {
        Logger::Log("HTTP Server: Failed to create thread");
        m_isRunning = false;
        closesocket(m_serverSocket);
        WSACleanup();
        m_wsaInitialized = false;
        return false;
    }

    Logger::Log("HTTP Server: Started on port %d", port);
    return true;
}

void HttpServer::Stop()
{
    if (!m_isRunning.load()) {
        return;
    }

    Logger::Log("HTTP Server: Stopping...");
    m_isRunning = false;

    // 关闭socket（这会中断accept调用）
    if (m_serverSocket != INVALID_SOCKET) {
        closesocket(m_serverSocket);
        m_serverSocket = INVALID_SOCKET;
    }

    // 等待线程结束
    if (m_threadHandle != NULL) {
        WaitForSingleObject(m_threadHandle, 5000);
        CloseHandle(m_threadHandle);
        m_threadHandle = NULL;
    }

    // 清理WSA
    if (m_wsaInitialized) {
        WSACleanup();
        m_wsaInitialized = false;
    }

    Logger::Log("HTTP Server: Stopped");
}

DWORD WINAPI HttpServer::ServerThreadProc(LPVOID lpParam)
{
    HttpServer* pServer = (HttpServer*)lpParam;
    pServer->ServerThread();
    return 0;
}

void HttpServer::ServerThread()
{
    Logger::Log("HTTP Server: Thread started");

    while (m_isRunning.load()) {
        // 设置socket为非阻塞模式，以便可以检查m_isRunning
        u_long mode = 1;
        ioctlsocket(m_serverSocket, FIONBIO, &mode);

        sockaddr_in clientAddr;
        int clientAddrLen = sizeof(clientAddr);
        SOCKET clientSocket = accept(m_serverSocket, (sockaddr*)&clientAddr, &clientAddrLen);

        if (clientSocket == INVALID_SOCKET) {
            int error = WSAGetLastError();
            if (error == WSAEWOULDBLOCK) {
                // 没有连接，继续循环
                Sleep(10);
                continue;
            }
            if (error == WSAEINTR || !m_isRunning.load()) {
                // 被中断或服务器停止
                break;
            }
            Logger::Log("HTTP Server: Accept failed: %d", error);
            continue;
        }

        // 恢复阻塞模式
        mode = 0;
        ioctlsocket(clientSocket, FIONBIO, &mode);

        // 处理客户端请求（在新线程中处理，支持并发）
        std::thread clientThread(&HttpServer::HandleClient, this, clientSocket);
        clientThread.detach();
    }

    Logger::Log("HTTP Server: Thread ended");
}

void HttpServer::HandleClient(SOCKET clientSocket)
{
    char buffer[8192] = {0};
    int bytesReceived = recv(clientSocket, buffer, sizeof(buffer) - 1, 0);

    if (bytesReceived <= 0) {
        closesocket(clientSocket);
        return;
    }

    buffer[bytesReceived] = '\0';
    std::string request(buffer);

    // 解析请求
    HttpRequest req;
    if (!ParseRequest(request, req)) {
        std::string response = BuildErrorResponse(400, "Bad Request");
        send(clientSocket, response.c_str(), (int)response.length(), 0);
        closesocket(clientSocket);
        return;
    }

    // 处理API请求
    std::string response = HandleAPI(req);
    send(clientSocket, response.c_str(), (int)response.length(), 0);
    closesocket(clientSocket);
}

bool HttpServer::ParseRequest(const std::string& request, HttpRequest& req)
{
    std::istringstream iss(request);
    std::string line;

    // 解析第一行（方法、路径、HTTP版本）
    if (!std::getline(iss, line)) {
        return false;
    }

    std::istringstream firstLine(line);
    firstLine >> req.method;
    
    std::string pathAndQuery;
    firstLine >> pathAndQuery;
    
    // 分离路径和查询参数
    size_t queryPos = pathAndQuery.find('?');
    if (queryPos != std::string::npos) {
        req.path = pathAndQuery.substr(0, queryPos);
        req.query = pathAndQuery.substr(queryPos + 1);
    } else {
        req.path = pathAndQuery;
    }

    // 解析头部
    while (std::getline(iss, line) && line != "\r" && !line.empty()) {
        size_t colonPos = line.find(':');
        if (colonPos != std::string::npos) {
            std::string key = line.substr(0, colonPos);
            std::string value = line.substr(colonPos + 1);
            // 去除前后空格
            key.erase(0, key.find_first_not_of(" \t\r\n"));
            key.erase(key.find_last_not_of(" \t\r\n") + 1);
            value.erase(0, value.find_first_not_of(" \t\r\n"));
            value.erase(value.find_last_not_of(" \t\r\n") + 1);
            req.headers[key] = value;
        }
    }

    // 解析请求体（如果有）
    if (req.method == "POST" || req.method == "PUT") {
        std::ostringstream bodyStream;
        std::string bodyLine;
        while (std::getline(iss, bodyLine)) {
            bodyStream << bodyLine << "\n";
        }
        req.body = bodyStream.str();
        if (!req.body.empty() && req.body.back() == '\n') {
            req.body.pop_back();
        }
    }

    // 解析查询参数和POST参数
    if (!req.query.empty()) {
        std::vector<std::string> params = SplitString(req.query, '&');
        for (const auto& param : params) {
            size_t eqPos = param.find('=');
            if (eqPos != std::string::npos) {
                std::string key = URLDecode(param.substr(0, eqPos));
                std::string value = URLDecode(param.substr(eqPos + 1));
                req.params[key] = value;
            }
        }
    }

    // 解析POST JSON参数
    if (req.method == "POST" && !req.body.empty()) {
        // 简单JSON解析（仅支持基本格式）
        std::string contentType = req.headers.count("Content-Type") ? req.headers["Content-Type"] : "";
        if (contentType.find("application/json") != std::string::npos) {
            // 提取JSON中的键值对
            std::string json = req.body;
            // 移除花括号和空格
            json.erase(std::remove(json.begin(), json.end(), '{'), json.end());
            json.erase(std::remove(json.begin(), json.end(), '}'), json.end());
            json.erase(std::remove(json.begin(), json.end(), ' '), json.end());
            json.erase(std::remove(json.begin(), json.end(), '\n'), json.end());
            json.erase(std::remove(json.begin(), json.end(), '\r'), json.end());
            json.erase(std::remove(json.begin(), json.end(), '\t'), json.end());

            std::vector<std::string> pairs = SplitString(json, ',');
            for (const auto& pair : pairs) {
                size_t colonPos = pair.find(':');
                if (colonPos != std::string::npos) {
                    std::string key = pair.substr(0, colonPos);
                    std::string value = pair.substr(colonPos + 1);
                    // 移除引号
                    if (key.front() == '"' && key.back() == '"') {
                        key = key.substr(1, key.length() - 2);
                    }
                    if (value.front() == '"' && value.back() == '"') {
                        value = value.substr(1, value.length() - 2);
                    }
                    req.params[key] = value;
                }
            }
        }
    }

    return true;
}

std::string HttpServer::BuildResponse(int statusCode, const std::string& contentType, const std::string& body)
{
    std::ostringstream oss;
    oss << "HTTP/1.1 " << statusCode << " ";
    
    switch (statusCode) {
        case 200: oss << "OK"; break;
        case 400: oss << "Bad Request"; break;
        case 404: oss << "Not Found"; break;
        case 500: oss << "Internal Server Error"; break;
        default: oss << "Unknown"; break;
    }
    
    oss << "\r\n";
    oss << "Content-Type: " << contentType << "\r\n";
    oss << "Content-Length: " << body.length() << "\r\n";
    oss << "Access-Control-Allow-Origin: *\r\n";  // 支持CORS
    oss << "Access-Control-Allow-Methods: GET, POST, OPTIONS\r\n";
    oss << "Access-Control-Allow-Headers: Content-Type\r\n";
    oss << "Connection: close\r\n";
    oss << "\r\n";
    oss << body;
    
    return oss.str();
}

std::string HttpServer::BuildJSONResponse(const std::string& json)
{
    return BuildResponse(200, "application/json; charset=utf-8", json);
}

std::string HttpServer::BuildErrorResponse(int statusCode, const std::string& message)
{
    std::ostringstream json;
    json << "{\"error\":\"" << EscapeJSON(message) << "\",\"code\":" << statusCode << "}";
    return BuildResponse(statusCode, "application/json; charset=utf-8", json.str());
}

std::string HttpServer::HandleAPI(const HttpRequest& req)
{
    // 处理OPTIONS请求（CORS预检）
    if (req.method == "OPTIONS") {
        return BuildResponse(200, "text/plain", "");
    }

    // 路由处理
    if (req.path == "/api/devices" && req.method == "GET") {
        return HandleGetDevices();
    }
    else if (req.path == "/api/scan" && req.method == "POST") {
        return HandleScan(req);
    }
    else if (req.path == "/api/scan/batch" && req.method == "POST") {
        return HandleScanBatch(req);
    }
    else if (req.path == "/api/status" && req.method == "GET") {
        return HandleGetStatus();
    }
    else if (req.path.find("/api/scan/result/") == 0 && req.method == "GET") {
        return HandleGetScanResult(req);
    }
    else {
        return BuildErrorResponse(404, "Not Found");
    }
}

std::string HttpServer::HandleGetDevices()
{
    char buffer[4096] = {0};
    int result = zhx_GetDevicesList(buffer, sizeof(buffer));
    
    if (result == 1) {
        return BuildJSONResponse(std::string(buffer));
    } else {
        return BuildErrorResponse(500, "Failed to get devices list");
    }
}

std::string HttpServer::HandleScan(const HttpRequest& req)
{
    if (!gpTwainApplicationCMD) {
        return BuildErrorResponse(500, "TWAIN not initialized");
    }

    // 获取参数
    int deviceId = 1;
    std::string outputPath = "./output";
    int timeoutMs = 30000;

    if (req.params.count("deviceId")) {
        deviceId = std::stoi(req.params["deviceId"]);
    }
    if (req.params.count("outputPath")) {
        outputPath = req.params["outputPath"];
    }
    if (req.params.count("timeoutMs")) {
        timeoutMs = std::stoi(req.params["timeoutMs"]);
    }

    // 调用扫描函数
    const char* path = outputPath.empty() ? nullptr : outputPath.c_str();
    int result = zhx_ScanComplete(deviceId, path, timeoutMs);

    std::ostringstream json;
    if (result == 1) {
        json << "{\"success\":true,\"message\":\"Scan completed\",\"outputPath\":\"" 
             << EscapeJSON(outputPath) << "\"}";
    } else {
        json << "{\"success\":false,\"message\":\"Scan failed or timeout\"}";
    }

    return BuildJSONResponse(json.str());
}

std::string HttpServer::HandleScanBatch(const HttpRequest& req)
{
    if (!gpTwainApplicationCMD) {
        return BuildErrorResponse(500, "TWAIN not initialized");
    }

    // 获取参数
    int deviceId = 1;
    std::string outputPath = "./output";
    int timeoutMs = 30000;
    int count = 1;  // 连续扫描次数

    if (req.params.count("deviceId")) {
        deviceId = std::stoi(req.params["deviceId"]);
    }
    if (req.params.count("outputPath")) {
        outputPath = req.params["outputPath"];
    }
    if (req.params.count("timeoutMs")) {
        timeoutMs = std::stoi(req.params["timeoutMs"]);
    }
    if (req.params.count("count")) {
        count = std::stoi(req.params["count"]);
    }

    // 执行连续扫描
    std::vector<std::string> results;
    for (int i = 0; i < count; i++) {
        std::string currentPath = outputPath;
        if (count > 1) {
            currentPath += "_" + std::to_string(i + 1);
        }

        const char* path = currentPath.empty() ? nullptr : currentPath.c_str();
        int result = zhx_ScanComplete(deviceId, path, timeoutMs);
        
        if (result == 1) {
            results.push_back(currentPath);
        } else {
            break;  // 失败则停止
        }
    }

    std::ostringstream json;
    json << "{\"success\":" << (results.size() == count ? "true" : "false") 
         << ",\"count\":" << results.size() 
         << ",\"results\":[";
    
    bool first = true;
    for (const auto& path : results) {
        if (!first) json << ",";
        json << "\"" << EscapeJSON(path) << "\"";
        first = false;
    }
    
    json << "]}";
    
    return BuildJSONResponse(json.str());
}

std::string HttpServer::HandleGetStatus()
{
    std::ostringstream json;
    json << "{\"server\":\"running\",\"port\":" << m_port;
    
    if (gpTwainApplicationCMD) {
        json << ",\"twain\":{\"initialized\":true,\"dsmState\":" 
             << gpTwainApplicationCMD->m_DSMState << "}";
    } else {
        json << ",\"twain\":{\"initialized\":false}";
    }
    
    json << "}";
    return BuildJSONResponse(json.str());
}

std::string HttpServer::HandleGetScanResult(const HttpRequest& req)
{
    // 提取结果ID（简单实现，实际应该维护一个结果列表）
    return BuildErrorResponse(501, "Not Implemented");
}

std::string HttpServer::URLDecode(const std::string& str)
{
    std::string result;
    for (size_t i = 0; i < str.length(); i++) {
        if (str[i] == '%' && i + 2 < str.length()) {
            int value;
            std::istringstream is(str.substr(i + 1, 2));
            if (is >> std::hex >> value) {
                result += (char)value;
                i += 2;
            } else {
                result += str[i];
            }
        } else if (str[i] == '+') {
            result += ' ';
        } else {
            result += str[i];
        }
    }
    return result;
}

std::string HttpServer::GetJSONValue(const std::string& json, const std::string& key)
{
    std::string searchKey = "\"" + key + "\":";
    size_t pos = json.find(searchKey);
    if (pos == std::string::npos) {
        return "";
    }
    
    pos += searchKey.length();
    while (pos < json.length() && (json[pos] == ' ' || json[pos] == ':')) {
        pos++;
    }
    
    if (pos >= json.length()) {
        return "";
    }
    
    if (json[pos] == '"') {
        pos++;
        size_t endPos = json.find('"', pos);
        if (endPos != std::string::npos) {
            return json.substr(pos, endPos - pos);
        }
    } else {
        size_t endPos = json.find_first_of(",}", pos);
        if (endPos != std::string::npos) {
            std::string value = json.substr(pos, endPos - pos);
            // 去除空格
            value.erase(0, value.find_first_not_of(" \t\r\n"));
            value.erase(value.find_last_not_of(" \t\r\n") + 1);
            return value;
        }
    }
    
    return "";
}

std::string HttpServer::EscapeJSON(const std::string& str)
{
    std::string result;
    for (char c : str) {
        switch (c) {
            case '"': result += "\\\""; break;
            case '\\': result += "\\\\"; break;
            case '\b': result += "\\b"; break;
            case '\f': result += "\\f"; break;
            case '\n': result += "\\n"; break;
            case '\r': result += "\\r"; break;
            case '\t': result += "\\t"; break;
            default: result += c; break;
        }
    }
    return result;
}

std::vector<std::string> HttpServer::SplitString(const std::string& str, char delimiter)
{
    std::vector<std::string> result;
    std::istringstream iss(str);
    std::string token;
    while (std::getline(iss, token, delimiter)) {
        result.push_back(token);
    }
    return result;
}

//////////////////////////////////////////////////////////////////////////////
// C接口实现
//////////////////////////////////////////////////////////////////////////////

extern "C" int zhx_HttpServer_Start(int port)
{
    if (g_pHttpServer == nullptr) {
        g_pHttpServer = new HttpServer();
    }
    
    if (g_pHttpServer->IsRunning()) {
        Logger::Log("HTTP Server: Already running");
        return 0;
    }
    
    if (g_pHttpServer->Start(port)) {
        return 1;
    }
    return 0;
}

extern "C" void zhx_HttpServer_Stop()
{
    if (g_pHttpServer != nullptr) {
        g_pHttpServer->Stop();
        delete g_pHttpServer;
        g_pHttpServer = nullptr;
    }
}

extern "C" int zhx_HttpServer_IsRunning()
{
    if (g_pHttpServer != nullptr && g_pHttpServer->IsRunning()) {
        return 1;
    }
    return 0;
}

extern "C" int zhx_HttpServer_GetPort()
{
    if (g_pHttpServer != nullptr) {
        return g_pHttpServer->GetPort();
    }
    return 0;
}

#endif // TWH_CMP_MSC
