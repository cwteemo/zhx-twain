#include "http_client.h"
#include <afxwin.h>

HttpClient::HttpClient() : m_timeout(30000) { // 默认30秒超时
    m_pSession = new CInternetSession(_T("MFC HTTP Client"));
}

HttpClient::~HttpClient() {
    if (m_pSession) {
        m_pSession->Close();
        delete m_pSession;
    }
}

void HttpClient::SetTimeout(DWORD timeout) {
    m_timeout = timeout;
}

bool HttpClient::Get(const CString& url, CString& response) {
    try {
        CHttpConnection* pServer = m_pSession->GetHttpConnection(url);
        if (!pServer) {
            return false;
        }

        CHttpFile* pFile = pServer->OpenRequest(CHttpConnection::HTTP_VERB_GET, _T("/"));
        if (!pFile) {
            delete pServer;
            return false;
        }

        // 设置超时
        pFile->SetOption(INTERNET_OPTION_CONNECT_TIMEOUT, m_timeout);
        pFile->SetOption(INTERNET_OPTION_RECEIVE_TIMEOUT, m_timeout);
        pFile->SetOption(INTERNET_OPTION_SEND_TIMEOUT, m_timeout);

        pFile->SendRequest();

        DWORD statusCode = 0;
        pFile->QueryInfoStatusCode(statusCode);

        if (statusCode == HTTP_STATUS_OK) {
            CString strLine;
            while (pFile->ReadString(strLine)) {
                response += strLine + _T("\n");
            }
        }

        delete pFile;
        delete pServer;
        return (statusCode == HTTP_STATUS_OK);
    }
    catch (CInternetException* pEx) {
        pEx->Delete();
        return false;
    }
}

bool HttpClient::Post(const CString& url, const CString& data, CString& response) {
    try {
        CHttpConnection* pServer = m_pSession->GetHttpConnection(url);
        if (!pServer) {
            return false;
        }

        CHttpFile* pFile = pServer->OpenRequest(CHttpConnection::HTTP_VERB_POST, _T("/"));
        if (!pFile) {
            delete pServer;
            return false;
        }

        // 设置超时
        pFile->SetOption(INTERNET_OPTION_CONNECT_TIMEOUT, m_timeout);
        pFile->SetOption(INTERNET_OPTION_RECEIVE_TIMEOUT, m_timeout);
        pFile->SetOption(INTERNET_OPTION_SEND_TIMEOUT, m_timeout);

        // 设置请求头
        pFile->AddRequestHeaders(_T("Content-Type: application/x-www-form-urlencoded"));

        // 发送请求
        pFile->SendRequest(data);

        DWORD statusCode = 0;
        pFile->QueryInfoStatusCode(statusCode);

        if (statusCode == HTTP_STATUS_OK) {
            CString strLine;
            while (pFile->ReadString(strLine)) {
                response += strLine + _T("\n");
            }
        }

        delete pFile;
        delete pServer;
        return (statusCode == HTTP_STATUS_OK);
    }
    catch (CInternetException* pEx) {
        pEx->Delete();
        return false;
    }
} 