#pragma once

#include <afxwin.h>
#include <afxinet.h>
#include <afxsock.h>

// 在CYourMainDlg类的头文件中，定义一个消息宏
#define WM_RECEIVE_DATA (WM_USER + 100)


// 声明主对话框类
class MainChannel : public CDialogEx
{
public:
    MainChannel(CWnd* pParent = nullptr);
    virtual ~MainChannel();

    // 对话框数据
    enum { IDD = IDD_YOUR_DIALOG };

    // 发送数据方法
    BOOL SendDataToTarget(const CString& data, HWND hTargetWnd);

protected:
    virtual BOOL OnInitDialog();
    
    // 消息处理函数
    afx_msg LRESULT OnReceiveData(WPARAM wParam, LPARAM lParam);
    
    DECLARE_MESSAGE_MAP()
};
