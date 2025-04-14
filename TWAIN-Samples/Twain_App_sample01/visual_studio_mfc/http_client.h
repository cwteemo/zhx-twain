#pragma once

#include <afxwin.h>
#include <afxinet.h>
#include <string>

class HttpClient {
public:
    HttpClient();
    ~HttpClient();

    // 发送GET请求
    bool Get(const CString& url, CString& response);
    
    // 发送POST请求
    bool Post(const CString& url, const CString& data, CString& response);

    // 设置超时时间（毫秒）
    void SetTimeout(DWORD timeout);

private:
    CInternetSession* m_pSession;
    DWORD m_timeout;
}; 