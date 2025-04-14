#include "http_server.h"
#include <afxwin.h>
#include <afxinet.h>
#include <afxsock.h>
#include <exception>
#include "../src/logger.h"

// 前向声明
class CInternetServerContext;

// 自定义HTTP服务器类
class CInternetServer : public CObject {
public:
    CInternetServer(CInternetSession* pSession) : m_pSession(pSession) {}
    virtual ~CInternetServer() {}

    bool Create(int port) {
        // 创建服务器套接字
        return m_socket.Create(port) && m_socket.Listen(5);
    }

    void Close() {
        m_socket.Close();
    }

    CInternetServerContext* GetContext();

protected:
    CInternetSession* m_pSession;
    CSocket m_socket;
};

// 自定义HTTP服务器上下文类
class CInternetServerContext : public CObject {
public:
    CInternetServerContext(CInternetServer* pServer, CSocket& socket) 
        : m_pServer(pServer), m_socket(socket) {}
    virtual ~CInternetServerContext() {
        m_socket.Close();
    }

    CString GetRequest() {
        char buffer[1024];
        int bytesRead = m_socket.Receive(buffer, sizeof(buffer) - 1);
        if (bytesRead > 0) {
            buffer[bytesRead] = '\0';
            return CString(buffer);
        }
        return _T("");
    }

    void WriteString(const CString& str) {
        m_socket.Send((LPCTSTR)str, str.GetLength() * sizeof(TCHAR));
    }

    void Close() {
        m_socket.Close();
    }

protected:
    CInternetServer* m_pServer;
    CSocket& m_socket;
};

// 实现 GetContext 方法
CInternetServerContext* CInternetServer::GetContext() {
    CSocket clientSocket;
    if (m_socket.Accept(clientSocket)) {
        return new CInternetServerContext(this, clientSocket);
    }
    return nullptr;
}

// Declare 'lastResponse' as a CString
CString lastResponse;

// Initialize 'lastResponse' with a default value or fetch from a relevant source
// Example: lastResponse = GetLastResponse(); // Assuming GetLastResponse() is a function that retrieves the last response

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
            
            // 解析HTTP请求中的参数
            std::string url = "/";
            std::string params = "";
            
            // 查找第一个空格（第一个空格之后到第二个空格之前是URL）
            size_t firstSpacePos = request.find(" ");
            if (firstSpacePos != std::string::npos) {
                size_t secondSpacePos = request.find(" ", firstSpacePos + 1);
                if (secondSpacePos != std::string::npos) {
                    // 提取URL（不包括HTTP参数）
                    std::string fullURL = request.substr(firstSpacePos + 1, secondSpacePos - firstSpacePos - 1);
                    Logger::Log("Full URL from request: %s", fullURL.c_str());
                    
                    // 查找URL中的参数（以?开始）
                    size_t paramPos = fullURL.find("?");
                    if (paramPos != std::string::npos) {
                        url = fullURL.substr(0, paramPos);
                        params = fullURL.substr(paramPos + 1);
                        Logger::Log("URL path: %s, Parameters: %s", url.c_str(), params.c_str());
                    } else {
                        url = fullURL;
                        Logger::Log("URL path with no parameters: %s", url.c_str());
                    }
                }
            }
            
            // 同时从POST请求体中解析参数
            size_t bodyPos = request.find("\r\n\r\n");
            if (bodyPos != std::string::npos && bodyPos + 4 < request.length()) {
                std::string body = request.substr(bodyPos + 4);
                if (!body.empty()) {
                    Logger::Log("Request body: %s", body.c_str());
                    
                    // 检查是否为 form-data 格式
                    if (request.find("Content-Type: multipart/form-data") != std::string::npos) {
                        Logger::Log("Detected multipart/form-data request");
                        
                        // 解析 form-data 格式
                        std::string boundary;
                        size_t boundaryPos = request.find("boundary=");
                        if (boundaryPos != std::string::npos) {
                            size_t boundaryEnd = request.find("\r\n", boundaryPos);
                            if (boundaryEnd != std::string::npos) {
                                boundary = request.substr(boundaryPos + 9, boundaryEnd - boundaryPos - 9);
                                Logger::Log("Form boundary: %s", boundary.c_str());
                                
                                // 寻找 "handle" 字段
                                std::string searchName = "name=\"handle\"";
                                size_t handlePos = body.find(searchName);
                                if (handlePos != std::string::npos) {
                                    // 找到值的开始位置
                                    size_t valueStart = body.find("\r\n\r\n", handlePos);
                                    if (valueStart != std::string::npos) {
                                        valueStart += 4; // 跳过 \r\n\r\n
                                        
                                        // 找到值的结束位置
                                        size_t valueEnd = body.find(boundary, valueStart);
                                        if (valueEnd != std::string::npos && valueEnd > valueStart) {
                                            // 提取值
                                            std::string value = body.substr(valueStart, valueEnd - valueStart - 2); // -2 去掉末尾的 \r\n
                                            Logger::Log("Found handle value: %s", value.c_str());
                                            
                                            // 将提取的值添加到params
                                            if (params.empty()) {
                                                params = "handle=" + value;
                                            } else {
                                                params += "&handle=" + value;
                                            }
                                        }
                                    }
                                }
                            }
                        }
                    } else if (request.find("Content-Type: application/x-www-form-urlencoded") != std::string::npos) {
                        // 标准表单编码
                        if (params.empty()) {
                            params = body;
                        } else {
                            params += "&" + body;
                        }
                    } else {
                        // 其他格式，默认添加
                        if (params.empty()) {
                            params = body;
                        } else {
                            params += "&" + body;
                        }
                    }
                    
                    Logger::Log("Combined parameters (URL + body): %s", params.c_str());
                }
            }
            
            bool messageSent = false;
            
            // 发送消息到主窗口 - 再次验证窗口有效性
            if (hMainWnd && ::IsWindow(hMainWnd)) {
                Logger::Log("Posting WM_HTTP_REQUEST message to main window (handle: %p)", hMainWnd);
                
                try {
                    // 创建包含URL和参数的格式化消息
                    CString messageContent;
                    if (!params.empty()) {
                        messageContent.Format(_T("URL: %s\r\nParams: %s"), 
                                            url.c_str(), 
                                            params.c_str());
                        
                        // 检查是否包含handle=scanners参数，特别标记这种请求
                        if (params.find("handle=scanners") != std::string::npos) {
                            messageContent += _T("\r\nRequestType: ScannerList");
                            Logger::Log("Adding ScannerList request type marker to message");
                        }
                        // 检查 form-data 中是否包含 handle=scanners
                        else if (request.find("name=\"handle\"") != std::string::npos && 
                                request.find("scanners") != std::string::npos) {
                            messageContent += _T("\r\nRequestType: ScannerList");
                            Logger::Log("Adding ScannerList request type marker for form-data");
                        }
                    } else {
                        messageContent.Format(_T("URL: %s\r\nNo parameters"), 
                                            url.c_str());
                    }
                    
                    CString* pRequest = new CString(messageContent);
                    if (!pRequest) {
                        Logger::Log("Failed to allocate memory for request");
                    } else {
                        // 检查是否包含scanners关键词
                        bool containsScanners = (pRequest->Find(_T("scanners")) != -1);
                        
                        BOOL result = ::PostMessage(hMainWnd, WM_HTTP_REQUEST, (WPARAM)pRequest, 0);
                        if (!result) {
                            Logger::Log("PostMessage failed. Error: %d", GetLastError());
                            delete pRequest;  // 确保内存被释放
                            pRequest = nullptr;
                        } else {
                            Logger::Log("PostMessage succeeded");
                            messageSent = true;
                            
                            // 等待一小段时间，让主窗口有机会处理消息
                            Sleep(100);
                        }
                        
                        // 保存是否包含scanners关键词的结果，用于构建响应
                        if (containsScanners) {
                            Logger::Log("Request contains 'scanners' keyword");
                        }
                    }
                } catch (std::exception& e) {
                    Logger::Log("Exception while posting message: %s", e.what());
                } catch (...) {
                    Logger::Log("Unknown exception while posting message");
                }
            } else {
                Logger::Log("No valid main window handle available (handle: %p, IsWindow: %d)", 
                        hMainWnd, hMainWnd ? ::IsWindow(hMainWnd) : 0);
            }
            
            // 无论主窗口是否处理了消息，都发送HTTP响应
            std::string response;

            // 检查请求是否包含handle=scanners参数
            bool scannerListRequested = false;

            // 优先检查参数中的handle=scanners
            if (params.find("handle=scanners") != std::string::npos) {
                scannerListRequested = true;
                Logger::Log("Scanner list explicitly requested via params");
            }

            // 检查是否在 form-data 中传递了 handle=scanners
            if (!scannerListRequested && 
                request.find("name=\"handle\"") != std::string::npos && 
                request.find("scanners") != std::string::npos) {
                scannerListRequested = true;
                Logger::Log("Scanner list requested via form-data");
            }

            // 如果参数中没有找到，检查整个请求
            if (!scannerListRequested && request.find("handle=scanners") != std::string::npos) {
                scannerListRequested = true;
                Logger::Log("Scanner list requested detected in full request");
            }

            Logger::Log("Scanner list requested: %s", scannerListRequested ? "YES" : "NO");

            // 检查请求是否包含scanners关键词但不是handle=scanners参数
            bool containsScannersKeyword = (!scannerListRequested && (request.find("scanners") != std::string::npos));
            Logger::Log("Contains 'scanners' keyword: %s", containsScannersKeyword ? "YES" : "NO");
            
            if (scannerListRequested) {
                // 处理handle=scanners请求
                std::string scannerResponse;
                
                // 使用传递给函数的HttpServer实例
                Logger::Log("Using HttpServer instance from parameters: %p", pHttpServer);
                
                // 验证HttpServer实例
                CString lastResponse;
                if (pHttpServer) {
                    // 直接从HttpServer实例获取最后的响应
                    lastResponse = pHttpServer->GetLastResponse();
                    Logger::Log("Retrieved last response directly from HttpServer instance: %s", (LPCTSTR)lastResponse);
                } else {
                    Logger::Log("Invalid HttpServer instance");
                    
                    // 尝试从窗口用户数据获取HttpServer实例（备用方法）
                    if (hMainWnd && ::IsWindow(hMainWnd)) {
                        HttpServer* pBackupServer = (HttpServer*)::GetWindowLongPtr(hMainWnd, GWLP_USERDATA);
                        if (pBackupServer) {
                            lastResponse = pBackupServer->GetLastResponse();
                            Logger::Log("Retrieved last response from window user data backup: %s", (LPCTSTR)lastResponse);
                        } else {
                            Logger::Log("Failed to get HttpServer instance from window user data");
                        }
                    } else {
                        Logger::Log("Invalid main window handle, cannot get HttpServer instance");
                    }
                }
                
                if (!lastResponse.IsEmpty()) {
                    // 将CString转换为std::string - 处理UNICODE/MBCS差异
                    #ifdef _UNICODE
                    // 如果是Unicode版本，需要转换
                    int len = lastResponse.GetLength();
                    char* buffer = new char[len*2 + 1]; // 为安全起见乘以2
                    // 转换宽字符到多字节字符
                    int size = WideCharToMultiByte(CP_ACP, 0, lastResponse.GetBuffer(), -1,
                                                    buffer, len*2 + 1, NULL, NULL);
                    lastResponse.ReleaseBuffer();
                    if (size > 0) {
                        scannerResponse = buffer;
                    } else {
                        scannerResponse = "SCANNERS:Conversion error";
                        Logger::Log("Error converting Unicode to ASCII");
                    }
                    delete[] buffer; // 移动到这里确保无论如何都会释放
                    #else
                    // 非Unicode版本可以直接转换
                    scannerResponse = (LPCSTR)lastResponse;
                    #endif
                    
                    Logger::Log("Returning scanner list: %s", scannerResponse.c_str());
                } else {
                    // 尝试从应用程序配置文件中获取备份扫描仪列表
                    CString backupList = AfxGetApp()->GetProfileString(_T("Settings"), _T("LastScannerList"), _T(""));
                    if (!backupList.IsEmpty()) {
                        Logger::Log("Using backup scanner list from app profile: %s", (LPCTSTR)backupList);
                        
                        #ifdef _UNICODE
                        // 转换备份列表
                        int len = backupList.GetLength();
                        char* buffer = new char[len*2 + 1]; // 为安全起见乘以2
                        int size = WideCharToMultiByte(CP_ACP, 0, backupList.GetBuffer(), -1,
                                                       buffer, len*2 + 1, NULL, NULL);
                        backupList.ReleaseBuffer();
                        if (size > 0) {
                            scannerResponse = buffer;
                        } else {
                            scannerResponse = "SCANNERS:Backup conversion error";
                        }
                        delete[] buffer;
                        #else
                        scannerResponse = (LPCSTR)backupList;
                        #endif
                    } else {
                        scannerResponse = "SCANNERS:No data available";
                        Logger::Log("No scanner list available, returning default message");
                    }
                }
                
                response = "HTTP/1.1 200 OK\r\n"
                          "Content-Type: text/plain\r\n"
                          "Connection: close\r\n"
                          "\r\n" + scannerResponse;
            }
            else if (containsScannersKeyword) {
                // 特殊响应
                response = "HTTP/1.1 200 OK\r\n"
                          "Content-Type: text/plain\r\n"
                          "Connection: close\r\n"
                          "\r\n"
                          "123456";
            } else if (!params.empty()) {
                // 包含其他参数的响应
                std::string paramResponse = "HTTP/1.1 200 OK\r\n"
                                          "Content-Type: text/html\r\n"
                                          "Connection: close\r\n"
                                          "\r\n"
                                          "<html><body>"
                                          "<h1>TWAIN HTTP Server</h1>"
                                          "<p>Received parameters: " + params + "</p>"
                                          "</body></html>";
                response = paramResponse;
            } else {
                // 普通响应
                response = "HTTP/1.1 200 OK\r\n"
                          "Content-Type: text/html\r\n"
                          "Connection: close\r\n"
                          "\r\n"
                          "<html><body>"
                          "<h1>TWAIN HTTP Server</h1>"
                          "<p>Request received and processed by main window.</p>"
                          "</body></html>";
            }
            
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
