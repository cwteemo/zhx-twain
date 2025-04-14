#include "AfxMainChannel.h"
#include "..\src\Logger.h"

// 定义对话框资源ID (如果没有实际的资源ID，可能需要修改)
#ifndef IDD_YOUR_DIALOG
#define IDD_YOUR_DIALOG 102
#endif

// 实现消息映射
BEGIN_MESSAGE_MAP(MainChannel, CDialogEx)
    ON_MESSAGE(WM_RECEIVE_DATA, &MainChannel::OnReceiveData)
END_MESSAGE_MAP()

// 构造函数
MainChannel::MainChannel(CWnd* pParent /*=nullptr*/)
    : CDialogEx(IDD_YOUR_DIALOG, pParent)
{
    // 构造函数初始化代码
}

// 析构函数
MainChannel::~MainChannel()
{
    // 析构函数清理代码
}

// 对话框初始化
BOOL MainChannel::OnInitDialog()
{
    CDialogEx::OnInitDialog();

    // 初始化日志
    Logger::Log("MainChannel initialized");
    
    // 可以在这里添加额外的初始化代码
    
    return TRUE;  // 返回TRUE表示焦点设置完成
}

// 接收数据消息处理函数
LRESULT MainChannel::OnReceiveData(WPARAM wParam, LPARAM lParam)
{
    Logger::Log("MainChannel OnReceiveData");
    // 从wParam中获取接收到的数据
    CString* pReceivedData = (CString*)wParam;

    // 在UI上显示或处理接收到的数据
    AfxMessageBox(*pReceivedData);

    // 清理数据
    delete pReceivedData;

    return 0;
}


// 发送数据方法
BOOL CYourMainDlg::SendDataToTarget(const CString& data, HWND hTargetWnd)
{
    try
    {
        // 验证目标窗口句柄
        if (!::IsWindow(hTargetWnd))
        {
            Logger::Log("Error: Invalid target window handle");
            return FALSE;
        }

        // 分配内存并复制数据
        TCHAR* pszCopy = _tcsdup(data);
        if (!pszCopy)
        {
            Logger::Log("Error: Memory allocation failed");
            return FALSE;
        }

        // 发送消息
        if (!::PostMessage(hTargetWnd, WM_RECEIVE_DATA, 0, (LPARAM)pszCopy))
        {
            // 发送失败，释放内存
            free(pszCopy);
            Logger::Log("Error: Failed to post message to target window");
            return FALSE;
        }

        Logger::Log("Data sent successfully: %s", (LPCTSTR)data);
        return TRUE;
    }
    catch (const std::exception& e)
    {
        Logger::Log("Exception in SendDataToTarget: %s", e.what());
        return FALSE;
    }
    catch (...)
    {
        Logger::Log("Unknown exception in SendDataToTarget");
        return FALSE;
    }
} 