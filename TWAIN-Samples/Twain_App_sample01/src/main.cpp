/***************************************************************************
* Copyright � 2007 TWAIN Working Group:  
*   Adobe Systems Incorporated, AnyDoc Software Inc., Eastman Kodak Company, 
*   Fujitsu Computer Products of America, JFL Peripheral Solutions Inc., 
*   Ricoh Corporation, and Xerox Corporation.
* All rights reserved.
*
* Redistribution and use in source and binary forms, with or without
* modification, are permitted provided that the following conditions are met:
*     * Redistributions of source code must retain the above copyright
*       notice, this list of conditions and the following disclaimer.
*     * Redistributions in binary form must reproduce the above copyright
*       notice, this list of conditions and the following disclaimer in the
*       documentation and/or other materials provided with the distribution.
*     * Neither the name of the TWAIN Working Group nor the
*       names of its contributors may be used to endorse or promote products
*       derived from this software without specific prior written permission.
*
* THIS SOFTWARE IS PROVIDED BY TWAIN Working Group ``AS IS'' AND ANY
* EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED
* WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
* DISCLAIMED. IN NO EVENT SHALL TWAIN Working Group BE LIABLE FOR ANY
* DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES
* (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES;
* LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND
* ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
* (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF THIS
* SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
*
***************************************************************************/

/**
* @file main.cpp
* The entry point to launch the application
* @author TWAIN Working Group
* @date October 2007
 */

#ifdef HAVE_CONFIG_H
#include <config.h>
#endif

#include "main.h"
#include "TwainString.h"
// I found that compiling using the sunfreeware.com stuff on Solaris 9
// required this typedef. This is related to the inclusion of signal.h
#if defined (__SVR4) && defined (__sun)
typedef union {
  long double  _q;
  uint32_t     _l[4];
} upad128_t;
#endif

#include <signal.h>

// Windows平台需要的头文件
#ifdef TWH_CMP_MSC
#include <windows.h>
#endif

#include "CommonTWAIN.h"
#include "TwainAppCMD.h"
#include "TwainApp_ui.h"
#include "Logger.h"
#include <time.h>  // 时间相关函数
#include <vector>
#include <string>
#include <algorithm>
#include "utilities.h"

using namespace std;

//////////////////////////////////////////////////////////////////////////////
// Global Variables
TwainAppCMD  *gpTwainApplicationCMD;  /**< The main application */
extern bool   gUSE_CALLBACKS;         // defined in TwainApp.cpp

//////////////////////////////////////////////////////////////////////////////
/** 
* Display exit message.
* @param[in] _sig not used.
*/
void onSigINT(int _sig)
{
  UNUSEDARG(_sig);
  cout << "\nGoodbye!" << endl;
  exit(0);
}
bool checkIfMorePagesAvailable();
// 新增辅助函数：确保目录存在，如果不存在则创建
bool EnsureDirectoryExists(const char* path);


//////////////////////////////////////////////////////////////////////////////
/** 
* Negotiate a capabilities between the app and the DS
* @param[in] _pCap the capabilities to negotiate
*/
void negotiate_CAP(const pTW_CAPABILITY _pCap)
{
  string input;

  // -Setting one cap could change another cap so always refresh the caps
  // before working with another one.  
  // -Another method of doing this is to let the DS worry about the state
  // of the caps instead of keeping a copy in the app like I'm doing.
  gpTwainApplicationCMD->initCaps();

  for (;;)
  {
    if((TWON_ENUMERATION == _pCap->ConType) || 
       (TWON_ONEVALUE == _pCap->ConType))
    {
      TW_MEMREF pVal = _DSM_LockMemory(_pCap->hContainer);

      // print the caps current value
      if(TWON_ENUMERATION == _pCap->ConType)
      {
        print_ICAP(_pCap->Cap, (pTW_ENUMERATION)(pVal));
      }
      else // TWON_ONEVALUE
      {
        print_ICAP(_pCap->Cap, (pTW_ONEVALUE)(pVal));
      }

      cout << "\nset cap# > ";
      cin >> input;
      cout << endl;

      if("q" == input)
      {
        _DSM_UnlockMemory(_pCap->hContainer);
        break;
      }
      else
      {
        int n = atoi(input.c_str());
        TW_UINT16  valUInt16 = 0;
		pTW_FIX32  pValFix32 = {0};
		pTW_FRAME  pValFrame = {0};

        // print the caps current value
        if(TWON_ENUMERATION == _pCap->ConType)
        {
          switch(((pTW_ENUMERATION)pVal)->ItemType)
          {
            case TWTY_UINT16:
              valUInt16 = ((pTW_UINT16)(&((pTW_ENUMERATION)pVal)->ItemList))[n];
            break;

            case TWTY_FIX32:
              pValFix32 = &((pTW_ENUMERATION_FIX32)pVal)->ItemList[n];
            break;
            
            case TWTY_FRAME:
              pValFrame = &((pTW_ENUMERATION_FRAME)pVal)->ItemList[n];
            break;
          }

          switch(_pCap->Cap)
          {
            case ICAP_PIXELTYPE:
              gpTwainApplicationCMD->set_ICAP_PIXELTYPE(valUInt16);
            break;

            case ICAP_BITDEPTH:
              gpTwainApplicationCMD->set_ICAP_BITDEPTH(valUInt16);
            break;

            case ICAP_UNITS:
              gpTwainApplicationCMD->set_ICAP_UNITS(valUInt16);
            break;
            
            case ICAP_XFERMECH:
              gpTwainApplicationCMD->set_ICAP_XFERMECH(valUInt16);
            break;
          
            case ICAP_XRESOLUTION:
            case ICAP_YRESOLUTION:
              gpTwainApplicationCMD->set_ICAP_RESOLUTION(_pCap->Cap, pValFix32);
            break;

            case ICAP_FRAMES:
              gpTwainApplicationCMD->set_ICAP_FRAMES(pValFrame);
            break;

            default:
              cerr << "Unsupported capability" << endl;
            break;
          }
        }
      }
      _DSM_UnlockMemory(_pCap->hContainer);
    }
    else
    {
      cerr << "Unsupported capability" << endl;
      break;
    }
  }

  return;
}

//////////////////////////////////////////////////////////////////////////////
/**
* drives main capabilities menu.  Negotiate all capabilities
*/
void negotiateCaps()
{
  // If the app is not in state 4, don't even bother showing this menu.
  if(gpTwainApplicationCMD->m_DSMState < 4)
  {
    cerr << "\nNeed to open a source first\n" << endl;
    return;
  }

  string input;

  // Loop forever until either SIGINT is heard or user types done to go back
  // to the main menu.
  for (;;)
  {
    printMainCaps();
    cout << "\n(h for help) > ";
    cin >> input;
    cout << endl;

    if("q" == input)
    {
      break;
    }
    else if("h" == input)
    {
      printMainCaps();
    }
    else if("1" == input)
    {
      negotiate_CAP(&(gpTwainApplicationCMD->m_ICAP_XFERMECH));
    }
    else if("2" == input)
    {
      negotiate_CAP(&(gpTwainApplicationCMD->m_ICAP_PIXELTYPE));
    }
    else if("3" == input)
    {
      negotiate_CAP(&(gpTwainApplicationCMD->m_ICAP_BITDEPTH));
    }
    else if("4" == input)
    {
      negotiate_CAP(&(gpTwainApplicationCMD->m_ICAP_XRESOLUTION));
    }
    else if("5" == input)
    {
      negotiate_CAP(&(gpTwainApplicationCMD->m_ICAP_YRESOLUTION));
    }
    else if("6" == input)
    {
      negotiate_CAP(&(gpTwainApplicationCMD->m_ICAP_FRAMES));
    }
    else if("7" == input)
    {
      negotiate_CAP(&(gpTwainApplicationCMD->m_ICAP_UNITS));
    }
    else
    {
      printMainCaps();
    }
  }

  return;
}

//////////////////////////////////////////////////////////////////////////////
/**
* Enables the source. The source will let us know when it is ready to scan by
* calling our registered callback function.
*/
void EnableDS()
{
  gpTwainApplicationCMD->m_DSMessage = 0;
  
  #ifdef TWNDS_OS_LINUX

    int test;
    sem_getvalue(&(gpTwainApplicationCMD->m_TwainEvent), &test);
    while(test<0)
    {
      sem_post(&(gpTwainApplicationCMD->m_TwainEvent));    // Event semaphore Handle
      sem_getvalue(&(gpTwainApplicationCMD->m_TwainEvent), &test);
    }
    while(test>0)
    {
      sem_wait(&(gpTwainApplicationCMD->m_TwainEvent)); // event semaphore handle
      sem_getvalue(&(gpTwainApplicationCMD->m_TwainEvent), &test);
    }

  #endif
  // -Enable the data source. This puts us in state 5 which means that we
  // have to wait for the data source to tell us to move to state 6 and
  // start the transfer.  Once in state 5, no more set ops can be done on the
  // caps, only get ops.
  // -The scan will not start until the source calls the callback function
  // that was registered earlier.
#ifdef TWNDS_OS_WIN
  if(!gpTwainApplicationCMD->enableDS(GetDesktopWindow(), FALSE))
#else
  if(!gpTwainApplicationCMD->enableDS(0, TRUE,callbackFunc))
#endif
  {
    return;
  }

#ifdef TWNDS_OS_WIN
  // now we have to wait until we hear something back from the DS.
  while(!gpTwainApplicationCMD->m_DSMessage)
  {
    TW_EVENT twEvent = {0};

    // If we are using callbacks, there is nothing to do here except sleep
    // and wait for our callback from the DS.  If we are not using them, 
    // then we have to poll the DSM.

    // Pumping messages is for Windows only
	  MSG Msg;
	  if(!GetMessage((LPMSG)&Msg, NULL, 0, 0))
    {
      break;//WM_QUIT
    }
    twEvent.pEvent = (TW_MEMREF)&Msg;

    twEvent.TWMessage = MSG_NULL;
    TW_UINT16  twRC = TWRC_NOTDSEVENT;
    twRC = _DSM_Entry( gpTwainApplicationCMD->getAppIdentity(),
                gpTwainApplicationCMD->getDataSource(),
                DG_CONTROL,
                DAT_EVENT,
                MSG_PROCESSEVENT,
                (TW_MEMREF)&twEvent);

    if(!gUSE_CALLBACKS && twRC==TWRC_DSEVENT)
    {
      // check for message from Source
      switch (twEvent.TWMessage)
      {
        case MSG_XFERREADY:
        case MSG_CLOSEDSREQ:
        case MSG_CLOSEDSOK:
        case MSG_NULL:
          gpTwainApplicationCMD->m_DSMessage = twEvent.TWMessage;
          break;

        default:
          cerr << "\nError - Unknown message in MSG_PROCESSEVENT loop\n" << endl;
          break;
      }
    }
    if(twRC!=TWRC_DSEVENT)
    {   
      TranslateMessage ((LPMSG)&Msg);
      DispatchMessage ((LPMSG)&Msg);
    }
  }
#elif defined(TWNDS_OS_LINUX)
  // Wait for the event be signaled
  sem_wait(&(gpTwainApplicationCMD->m_TwainEvent)); // event semaphore handle
                            // Indefinite wait
#endif

  // At this point the source has sent us a callback saying that it is ready to
  // transfer the image.

  if(gpTwainApplicationCMD->m_DSMessage == MSG_XFERREADY)
  {
    // move to state 6 as a result of the data source. We can start a scan now.
    gpTwainApplicationCMD->m_DSMState = 6;

    gpTwainApplicationCMD->startScan();
  }

  // Scan is done, disable the ds, thus moving us back to state 4 where we
  // can negotiate caps again.
  gpTwainApplicationCMD->disableDS();

  return;
}

//////////////////////////////////////////////////////////////////////////////
/**
* Callback funtion for DS.  This is a callback function that will be called by
* the source when it is ready for the application to start a scan. This 
* callback needs to be registered with the DSM before it can be called.
* It is important that the application returns right away after recieving this
* message.  Set a flag and return.  Do not process the callback in this function.
*/
#ifdef TWH_CMP_MSC
TW_UINT16 FAR PASCAL
#else
FAR PASCAL TW_UINT16 
#endif
DSMCallback(pTW_IDENTITY _pOrigin,
            pTW_IDENTITY _pDest,
            TW_UINT32    _DG,
            TW_UINT16    _DAT,
            TW_UINT16    _MSG,
            TW_MEMREF    _pData)
{
  UNUSEDARG(_pDest);
  UNUSEDARG(_DG);
  UNUSEDARG(_DAT);
  UNUSEDARG(_pData);

  TW_UINT16 twrc = TWRC_SUCCESS;

  // we are only waiting for callbacks from our datasource, so validate
  // that the originator.
  if(0 == _pOrigin ||
     _pOrigin->Id != gpTwainApplicationCMD->getDataSource()->Id)
  {
    return TWRC_FAILURE;
  }
  switch (_MSG)
  {
    case MSG_XFERREADY:
    case MSG_CLOSEDSREQ:
    case MSG_CLOSEDSOK:
    case MSG_NULL:
      gpTwainApplicationCMD->m_DSMessage = _MSG;
      // now signal the event semaphore
    #ifdef TWNDS_OS_LINUX
      {
      int test=12345;
      sem_post(&(gpTwainApplicationCMD->m_TwainEvent));    // Event semaphore Handle
  }
    #endif
      break;

    default:
      cerr << "Error - Unknown message in callback routine" << endl;
      twrc = TWRC_FAILURE;
      break;
  }

  return twrc;
}


//////////////////////////////////////////////////////////////////////////////
/**
* main program loop
*/
#ifdef TWH_CMP_MSC
int _tmain(int argc, _TCHAR* argv[])
#else
int main(int argc, char *argv[])
#endif
{
  UNUSEDARG(argc);
  UNUSEDARG(argv);
  int ret = EXIT_SUCCESS;
  return ret;
  Logger::Init();  // 初始化日志

  // Instantiate the TWAIN application CMD class
  HWND parentWindow = NULL;

#ifdef TWH_CMP_MSC
  parentWindow = GetConsoleWindow();
#endif
  gpTwainApplicationCMD = new TwainAppCMD(parentWindow);

  // setup a signal handler for SIGINT that will allow the program to stop
  signal(SIGINT, &onSigINT);

  string input;
  return ret;
  printOptions();

  // start the main event loop
  for (;;)
  {
    cout << "\n(h for help) > ";
    cin >> input;
    cout << endl;


    if("q" == input)
    {
      break;
    }
    else if("h" == input)
    {
      printOptions();
    }
    else if("cdsm" == input)
    {
      gpTwainApplicationCMD->connectDSM();
    }
    else if("xdsm" == input)
    {
      gpTwainApplicationCMD->disconnectDSM();
    }
    else if("lds" == input)
    {
      gpTwainApplicationCMD->printAvailableDataSources();
    }
    else if("pds" == input.substr(0,3))
    {
      gpTwainApplicationCMD->printIdentityStruct(atoi(input.substr(3,input.length()-3).c_str()));
    }
    else if("cds" == input.substr(0,3))
    {
      gpTwainApplicationCMD->loadDS(atoi(input.substr(3,input.length()-3).c_str()));
    }
    else if("xds" == input)
    {
      gpTwainApplicationCMD->unloadDS();
    }
    else if("caps" == input)
    {
      if(gpTwainApplicationCMD->m_DSMState < 3)
      {
        cout << "\nYou need to select a source first!" << endl;
      }
      else
      {
        negotiateCaps();
        printOptions();
      }
    }
    else if("scan" == input)
    {
      EnableDS();
    }
    else
    {
      // default action
      printOptions();
    }
  }

  gpTwainApplicationCMD->exit();
  delete gpTwainApplicationCMD;
  gpTwainApplicationCMD = 0;

  Logger::Cleanup();  // 清理日志
  return ret;
}


/**
 * 测试DLL接口是否可以被调用
 */
void zhx_twain_test() {
    
    std::cout << "Hello, World!\nThis message comes from a CPP function!" << std::endl;
    
    // 返回测试成功的字符串
}

void zhx_twain() {
    std::cout << "Hello, World!\nThis message comes from a CPP function!" << std::endl;
    int ret = EXIT_SUCCESS;
    Logger::Init();  // 初始化日志
    HWND parentWindow = NULL;
    std::cout << "A" << std::endl;
    Logger::Log("A");
    #ifdef TWH_CMP_MSC
    parentWindow = GetDesktopWindow();
    #endif
    std::cout << "B" << std::endl;
    Logger::Log("B");
    gpTwainApplicationCMD = new TwainAppCMD(parentWindow);
    signal(SIGINT, &onSigINT);
    std::cout << "C" << std::endl;
    Logger::Log("C");
    gpTwainApplicationCMD->connectDSM();
    std::cout << "D" << std::endl;
    Logger::Log("D");
    //gpTwainApplicationCMD->disconnectDSM();
    std::cout << "E" << std::endl;
    Logger::Log("E");
    //gpTwainApplicationCMD->printAvailableDataSources();
    const char* dataSource = gpTwainApplicationCMD->getAvailableDataSources();
    printf("[DLL INFO] dataSource Called, dataSource: %s\n", dataSource ? dataSource : "NULL");
    std::cout << "F" << std::endl;
    Logger::Log("F");
    //gpTwainApplicationCMD->printIdentityStruct(atoi("2"));
    std::cout << "G" << std::endl;
    Logger::Log("G");
    gpTwainApplicationCMD->loadDS(atoi("2"));
    std::cout << "H" << std::endl;
    Logger::Log("H");
    //gpTwainApplicationCMD->unloadDS();
    std::cout << "I" << std::endl;
    Logger::Log("I");
    EnableDS();
    std::cout << "J" << std::endl;
    Logger::Log("J");
    gpTwainApplicationCMD->exit();
    std::cout << "K" << std::endl;
    Logger::Log("K");
    delete gpTwainApplicationCMD;
    std::cout << "L" << std::endl;
    Logger::Log("L");
    gpTwainApplicationCMD = 0;
    std::cout << "M" << std::endl;
    Logger::Log("M");
     std::cout << "N" << std::endl;
     Logger::Log("N");
     Logger::Cleanup();  // 清理日志
}


/**
 * @brief 初始化TWAIN环境
 * 
 * 此函数负责创建TWAIN应用实例并连接到数据源管理器(DSM)。
 * 如果TWAIN环境已经初始化，会先清理现有资源再重新初始化。
 * 
 * 初始化流程包括：
 * 1. 检查并清理现有环境（如果存在）
 * 2. 创建新的TwainAppCMD实例
 * 3. 连接到数据源管理器(DSM)
 * 
 * @return 无返回值，通过日志记录初始化结果
 */
void zhx_Init() {
    // 记录函数调用
    Logger::Init();
    Logger::Log("@INFO zhx_Init called");

    try {
        // 检查是否已经初始化
        if (gpTwainApplicationCMD) {
            Logger::Log("@INFO TWAIN The environment has been initialized, re-initialized");
            
            // 清理现有资源
            if (gpTwainApplicationCMD->m_DSMState >= 5) {
                gpTwainApplicationCMD->disableDS();
            }
            if (gpTwainApplicationCMD->m_DSMState >= 4) {
                gpTwainApplicationCMD->unloadDS();
            }
            if (gpTwainApplicationCMD->m_DSMState >= 3) {
                gpTwainApplicationCMD->disconnectDSM();
            }
            delete gpTwainApplicationCMD;
            gpTwainApplicationCMD = NULL;
        }

        // 获取桌面窗口作为父窗口
        HWND parentWindow = NULL;
        #ifdef TWH_CMP_MSC
        parentWindow = GetDesktopWindow();
        #endif
        
        // 创建 TWAIN 应用实例
        gpTwainApplicationCMD = new TwainAppCMD(parentWindow);
        if (!gpTwainApplicationCMD) {
            Logger::Log("@ERROR Failed to create a TwainAppCMD instance");
            return; // 初始化失败
        }
        
        // 连接到 DSM
        gpTwainApplicationCMD->connectDSM();
        if (gpTwainApplicationCMD->m_DSMState < 3) {
            Logger::Log("@ERROR Failed to connect to DSM");
            delete gpTwainApplicationCMD;
            gpTwainApplicationCMD = NULL;
            return; // 初始化失败
        }
        //zhx_GetDevicesList();
        Logger::Log("@INFO TWAIN The environment is initialized successfully and the DSM is connected");
        Logger::Cleanup();
    }
    catch (std::exception& e) {
        Logger::Log("@ERROR zhx_Init An exception has occurred: %s", e.what());
        Logger::Cleanup();
        // 清理资源
        if (gpTwainApplicationCMD) {
            delete gpTwainApplicationCMD;
            gpTwainApplicationCMD = NULL;
        }
    }
    catch (...) {
        Logger::Log("@ERROR zhx_Init An unknown anomaly has occurred");
        Logger::Cleanup();
        // 清理资源
        if (gpTwainApplicationCMD) {
            delete gpTwainApplicationCMD;
            gpTwainApplicationCMD = NULL;
        }
    }
}


/**
 * @brief 获取可用的TWAIN扫描仪列表
 * 
 * 此函数返回JSON格式的扫描仪列表，包含各扫描仪的ID、名称和制造商信息。
 * 如果TWAIN环境未初始化或获取失败，将返回空JSON数组。
 * 
 * JSON格式示例：
 * [
 *   {"id":1,"name":"Scanner1","manufacturer":"Manufacturer1"},
 *   {"id":2,"name":"Scanner2","manufacturer":"Manufacturer2"}
 * ]
 * 
 * @return 返回JSON格式的扫描仪列表字符串，调用者负责使用free()释放返回的内存
 */
   char* zhx_GetDevicesList() {
       Logger::Init();
       Logger::Log("@INFO zhx_GetDevicesList called");
       
       // 检查TWAIN环境初始化状态
       if (!gpTwainApplicationCMD) {
           Logger::Log("@ERROR TWAIN not init");
           Logger::Cleanup();
           return _strdup("[]");
       }
       
       // 检查DSM连接状态
       if (gpTwainApplicationCMD->m_DSMState < 3) {
           Logger::Log("@ERROR DSM disconnect");
           Logger::Cleanup();
           return _strdup("[]");
       }
       
       // 确保扫描仪列表为空，避免断言错误
       // 这可能需要根据TwainApp的实现来调整
       // gpTwainApplicationCMD->refreshDataSources(); // 如果有这样的方法刷新数据源列表
       
       // 获取扫描仪列表
       char* result = _strdup(gpTwainApplicationCMD->getAvailableDataSources());
       Logger::Log("@INFO return list");
       Logger::Cleanup();
       return result;
   }

/**
 * @brief 根据扫描仪名称打开对应的数据源
 * 
 * 此函数通过扫描仪名称查找并打开相应的TWAIN数据源。
 * 扫描仪名称应与zhx_GetDevicesList()返回的JSON中的"name"字段匹配。
 * 
 * 操作流程：
 * 1. 解析当前可用扫描仪列表
 * 2. 查找匹配名称的扫描仪
 * 3. 加载找到的扫描仪数据源
 * 
 * @param device 扫描仪名称，从zhx_GetDevicesList返回的JSON中获取
 * @return 成功返回1，失败返回0
 */
int zhx_OpenDevice(char *device) {
    Logger::Init();
    Logger::Log("@INFO zhx_OpenDevice called with device: %s", device ? device : "NULL");
    printf("[DLL INFO] zhx_OpenDevice Called, scanner name: %s\n", device ? device : "NULL");
    
    // 检查参数
    if (!device || !*device) {
        Logger::Log("@ERROR The device name is empty");
        printf("[DLL ERROR] The device name is empty\n");
        Logger::Cleanup();
        return 0;
    }
    
    // 检查TWAIN环境是否初始化
    if (!gpTwainApplicationCMD) {
        Logger::Log("@ERROR The TWAIN environment is not initialized");
        printf("[DLL ERROR] The TWAIN environment is not initialized\n");
        Logger::Cleanup();
        return 0;
    }
    
    // 检查DSM是否已连接
    if (gpTwainApplicationCMD->m_DSMState < 3) {
        Logger::Log("@ERROR DSM is not connected");
        printf("[DLL ERROR] DSM is not connected\n");
        Logger::Cleanup();
        return 0;
    }
    
    try {
        // Get device number from the device name
        int deviceNumber = zhx_GetDeviceNumber(device);
        if (deviceNumber <= 0) {
            Logger::Log("@ERROR Could not find scanner named '%s'", device);
            printf("[DLL ERROR] Could not find scanner named '%s'\n", device);
            return 0;
        }

        // Load the found scanner using device number
        gpTwainApplicationCMD->loadDS(deviceNumber);

        // Verify scanner loaded successfully (should be in state 4)
        if (gpTwainApplicationCMD->m_DSMState != 4) {
            Logger::Log("@ERROR Failed to load scanner, current state: %d", gpTwainApplicationCMD->m_DSMState);
            printf("[DLL ERROR] Failed to load scanner\n");
            return 0;
        }

        Logger::Log("@INFO Successfully loaded scanner '%s'", device);
        printf("[DLL INFO] Successfully loaded scanner '%s'\n", device);
        Logger::Cleanup();
        return 1; // Success
    }
    catch (std::exception& e) {
        Logger::Log("@ERROR zhx_OpenDevice An exception has occurred: %s", e.what());
        printf("[DLL ERROR] open scanner An exception has occurred: %s\n", e.what());
        Logger::Cleanup();
        return 0;
    }
    catch (...) {
        Logger::Log("@ERROR zhx_OpenDevice An unknown anomaly has occurred");
        printf("[DLL ERROR] open scanner error\n");
        Logger::Cleanup();
        return 0;
    }
}



/**
 * @brief 根据扫描仪名称获取对应的设备编号
 * 
 * 此函数从可用扫描仪列表中查找指定名称的扫描仪，并返回其设备编号。
 * 设备编号是用于loadDS()方法的参数，从1开始计数。
 * 
 * @param device 扫描仪名称
 * @return 成功返回设备编号(>0)，失败返回0
 */
/**
 * @brief 根据扫描仪名称获取对应的设备编号
 * 
 * 此函数从可用扫描仪列表中查找指定名称的扫描仪，并返回其设备编号。
 * 设备编号是用于loadDS()方法的参数，从1开始计数。
 * 
 * @param device 扫描仪名称
 * @return 成功返回设备编号(>0)，失败返回0
 */
static int zhx_GetDeviceNumber(const char *device) {
    // 检查参数
    if (!device || !*device) {
        return 0;
    }
    
    // 检查TWAIN环境是否初始化
    if (!gpTwainApplicationCMD || gpTwainApplicationCMD->m_DSMState < 3) {
        return 0;
    }
    
    try {
        // Get scanner list (semicolon-separated string)
        const char* scannerList = gpTwainApplicationCMD->getAvailableDataSources();
        Logger::Log("@INFO Retrieved scanner list: %s", scannerList);

        // Find scanner name and get index
        int deviceIndex = -1;
        int currentIndex = 0;

        // Create a copy of the scanner list for parsing
        char* scannerListCopy = _strdup(scannerList);
        if (!scannerListCopy) {
            return 0;
        }

        // Use strtok to split the string
        char* scanner = strtok(scannerListCopy, ";");
        while (scanner != NULL) {
            // Compare scanner name
            if (strcmp(device, scanner) == 0) {
                deviceIndex = currentIndex;
                break;
            }
            
            // Move to next scanner
            scanner = strtok(NULL, ";");
            currentIndex++;
        }

        // Free temporary string copy
        free(scannerListCopy);

        // If matching scanner not found
        if (deviceIndex == -1) {
            return 0;
        }

        // Convert zero-based index to one-based device number
        int deviceNumber = deviceIndex + 1;
        Logger::Log("@INFO Found device at index %d, using device number %d", deviceIndex, deviceNumber);
        
        return deviceNumber; // Return the device number
    }
    catch (...) {
        return 0;
    }
}


// 在启动扫描前获取目录文件列表
std::vector<std::string> GetDirectoryFiles(const std::string& path) {
    std::vector<std::string> files;
    std::string searchPath = path;
    
    if (searchPath.back() != '/' && searchPath.back() != '\\') {
        searchPath += '/';
    }
    searchPath += "*.*";
    
    WIN32_FIND_DATAA findData;
    HANDLE hFind = FindFirstFileA(searchPath.c_str(), &findData);
    
    if (hFind != INVALID_HANDLE_VALUE) {
        do {
            // 忽略目录
            if (!(findData.dwFileAttributes & FILE_ATTRIBUTE_DIRECTORY)) {
                files.push_back(findData.cFileName);
            }
        } while (FindNextFileA(hFind, &findData));
        FindClose(hFind);
    }
    
    return files;
}



 /**
 * @brief 执行扫描操作并保存图像
 * 
 * 此函数设置保存路径，启动扫描过程，并通过文件系统监控获取实际生成的文件。
 * 
 * @param path 图像保存路径，如果为NULL则使用默认路径
 * @param cb 扫描回调函数，每扫描完成一页时调用，可为NULL
 * @param count 要扫描的页数，0表示不限制数量(扫描直到无纸或用户取消)，
 *              负数将被视为1，正数表示指定的页数
 * @return 成功返回实际扫描的页数，失败返回0
 */
int zhx_Scan(char *path, ScanCallback cb, int count) {
    // 记录函数调用
    Logger::Init();
    Logger::Log("@INFO zhx_Scan is called, path: %s, pages: %d", path ? path : "default", count);
    std::string serinumber = generateFilenameSafeSerialNumber(); // 例如：SCAN_20250422_213045_123
    // 检查TWAIN环境是否初始化
    if (!gpTwainApplicationCMD) {
        Logger::Log("@ERROR The TWAIN environment is not initialized");
        Logger::Cleanup();
        return 0;
    }
    
    // 检查数据源是否已连接
    if (gpTwainApplicationCMD->m_DSMState < 4) {
        Logger::Log("@ERROR Not connected to the scanning device, current state: %d", gpTwainApplicationCMD->m_DSMState);
        Logger::Cleanup();
        return 0;
    }
    
    // 处理count参数
    bool unlimitedScan = false;
    if (count == 0) {
        unlimitedScan = true;
        Logger::Log("@INFO Set to unlimited scan page mode");
    } else if (count < 0) {
        count = 1; // 将负数视为1
        Logger::Log("@INFO Negative pages are treated as 1 page");
    }
    
    // 设置并确保保存路径存在
    std::string basePath = ".";
    if (path && *path) {
        // 确保目录存在
        if (!EnsureDirectoryExists(path)) {
            Logger::Log("@ERROR Failed to create save directory: %s", path);
            Logger::Cleanup();
            return 0;
        }
        
        basePath = path;
        gpTwainApplicationCMD->setSavePath(path);
        Logger::Log("@INFO Set scan save path to: %s", path);
    } else {
        Logger::Log("@INFO Using default save path");
    }
    
    // 标准化路径格式
    std::replace(basePath.begin(), basePath.end(), '\\', '/');
    if (basePath.back() != '/') {
        basePath += '/';
    }
    
    int scannedPages = 0;
    
    try {
        // 多页扫描循环
        while (unlimitedScan || scannedPages < count) {
            Logger::Log("@INFO Starting scan for page %d %s", 
                        scannedPages + 1, 
                        unlimitedScan ? "(unlimited mode)" : "");
            
            // 获取扫描前的文件列表
            std::vector<std::string> filesBefore;
            WIN32_FIND_DATAA findData;
            std::string searchPattern = basePath + "*.bmp";
            HANDLE hFind = FindFirstFileA(searchPattern.c_str(), &findData);
            
            if (hFind != INVALID_HANDLE_VALUE) {
                do {
                    if (!(findData.dwFileAttributes & FILE_ATTRIBUTE_DIRECTORY)) {
                        filesBefore.push_back(findData.cFileName);
                    }
                } while (FindNextFileA(hFind, &findData));
                FindClose(hFind);
            }
            
            // 启动扫描
            EnableDS();
            
            // 获取扫描后的文件列表
            std::vector<std::string> filesAfter;
            hFind = FindFirstFileA(searchPattern.c_str(), &findData);
            
            if (hFind != INVALID_HANDLE_VALUE) {
                do {
                    if (!(findData.dwFileAttributes & FILE_ATTRIBUTE_DIRECTORY)) {
                        filesAfter.push_back(findData.cFileName);
                    }
                } while (FindNextFileA(hFind, &findData));
                FindClose(hFind);
            }
            
            // 找出新增的文件
            std::vector<std::string> newFiles;
            for (const auto& file : filesAfter) {
                if (std::find(filesBefore.begin(), filesBefore.end(), file) == filesBefore.end()) {
                    newFiles.push_back(file);
                }
            }
            
            // 记录扫描到的新文件数量
            if (newFiles.empty()) {
                Logger::Log("@WARN No new files were found after scan");
            } else {
                Logger::Log("@INFO Found %d new file(s) after scan", newFiles.size());
            }
            
            // 对每个新文件调用回调
            bool cancelScan = false;
            if (cb && !newFiles.empty()) {
                for (const auto& newFile : newFiles) {
                    std::string fullPath = basePath + newFile;
                    Logger::Log("@INFO Processing scanned file: %s", fullPath.c_str());
                    
                    int callbackResult = cb(const_cast<char*>(fullPath.c_str()));
                    Logger::Log("@INFO Callback for file %s returned: %d", newFile.c_str(), callbackResult);
                    
                    // 如果回调返回值小于等于0，视为用户请求取消
                    if (callbackResult <= 0) {
                        Logger::Log("@INFO Callback requested to cancel scanning");
                        cancelScan = true;
                        //break;
                    }
                }
            }
            
            // 更新扫描页数
            if (!newFiles.empty()) {
                scannedPages += (int)newFiles.size();
                Logger::Log("@INFO Total pages scanned so far: %d", scannedPages);
            } else {
                // 如果没有新文件但已执行扫描，增加计数并记录警告
                scannedPages++;
                Logger::Log("@WARN No new files detected, but counting page %d as scanned", scannedPages);
            }
            
            // 检查是否需要取消扫描
            if (cancelScan) {
                //break;
            }
            
            // 检查是否还有更多页可扫描（对于无限模式或剩余页数）
            if (unlimitedScan || scannedPages < count) {
                bool hasMorePages = checkIfMorePagesAvailable();
                
                if (!hasMorePages) {
                    Logger::Log("@INFO No more pages to scan, ending scan process");
                    break;
                }
                
                // 如果是无限模式，添加小延迟避免CPU占用过高
                if (unlimitedScan) {
                    Sleep(100);
                }
            }
        }
        
        Logger::Log("@INFO Scan completed successfully, total pages scanned: %d", scannedPages);
        Logger::Cleanup();
        return scannedPages;
    }
    catch (std::exception& e) {
        Logger::Log("@ERROR Exception during scan: %s", e.what());
        Logger::Cleanup();
        return scannedPages; // 返回已成功扫描的页数
    }
    catch (...) {
        Logger::Log("@ERROR Unknown exception during scan");
        Logger::Cleanup();
        return scannedPages; // 返回已成功扫描的页数
    }
}

// 确保目录存在，如果不存在则创建
bool EnsureDirectoryExists(const char* path) {
    if (!path || !*path) {
        return false;
    }
    
    // 检查目录是否存在
    DWORD attributes = GetFileAttributesA(path);
    if (attributes != INVALID_FILE_ATTRIBUTES && 
        (attributes & FILE_ATTRIBUTE_DIRECTORY)) {
        // 目录已存在
        return true;
    }
    
    // 创建完整的目录路径（支持多级目录）
    char tempPath[MAX_PATH];
    char* pszPath = NULL;
    strcpy_s(tempPath, MAX_PATH, path);
    size_t len = strlen(tempPath);
    
    // 如果路径末尾有斜杠，移除
    if (tempPath[len - 1] == '\\' || tempPath[len - 1] == '/') {
        tempPath[len - 1] = 0;
    }
    
    // 创建目录层次
    for (pszPath = tempPath; *pszPath; pszPath++) {
        if (*pszPath == '\\' || *pszPath == '/') {
            char savedChar = *pszPath;
            *pszPath = 0;  // 暂时终止字符串
            
            // 尝试创建当前级别目录
            if (GetFileAttributesA(tempPath) == INVALID_FILE_ATTRIBUTES) {
                if (!CreateDirectoryA(tempPath, NULL) && 
                    GetLastError() != ERROR_ALREADY_EXISTS) {
                    Logger::Log("@ERROR Failed to create directory: %s", tempPath);
                    return false;
                }
            }
            
            *pszPath = savedChar;  // 恢复字符
        }
    }
    
    // 创建最终目录
    if (GetFileAttributesA(tempPath) == INVALID_FILE_ATTRIBUTES) {
        if (!CreateDirectoryA(tempPath, NULL) && 
            GetLastError() != ERROR_ALREADY_EXISTS) {
            Logger::Log("@ERROR Failed to create directory: %s", tempPath);
            return false;
        }
    }
    
    Logger::Log("@INFO Directory exists or was created successfully: %s", path);
    return true;
}



/**
 * @brief 检查扫描设备是否还有更多页可扫描
 * 
 * 此函数需要根据TWAIN API实现，检查扫描设备状态
 * 特别是自动进纸器(ADF)中是否还有纸张
 * 
 * @return 有更多页返回true，否则返回false
 */
bool checkIfMorePagesAvailable() {
    // 这个函数需要根据TWAIN API实现
    // 可能需要查询CAP_FEEDERLOADED等能力
    // 简化示例，总是返回false表示没有更多页
    return true;
    
    // 实际实现可能类似:
    /*
    TW_CAPABILITY cap;
    cap.Cap = CAP_FEEDERLOADED;
    cap.ConType = TWON_ONEVALUE;
    
    if (gpTwainApplicationCMD->getScannerCapability(&cap) == TWRC_SUCCESS) {
        TW_ONEVALUE* val = (TW_ONEVALUE*)_DSM_LockMemory(cap.hContainer);
        bool hasMorePages = (val->Item != 0);
        _DSM_UnlockMemory(cap.hContainer);
        _DSM_Free(cap.hContainer);
        return hasMorePages;
    }
    
    return false; // 查询失败，假设没有更多页
    */
}

/**
 * @brief 结束扫描并卸载数据源
 * 
 * 此函数在完成扫描操作后调用，用于卸载当前加载的数据源。
 * 它会检查当前状态，确保以正确的顺序进行清理：
 * 1. 如果扫描仪处于已启用状态，先禁用它
 * 2. 然后卸载数据源
 * 
 * 此函数不会断开与DSM的连接，仅卸载数据源。
 * 
 * @return 无返回值
 */
void zhx_EndScan() {
    Logger::Init();
    Logger::Log("@INFO zhx_EndScan caller");
    printf("[DLL INFO] zhx_EndScan caller\n");
    
    try {
        // 检查TWAIN环境是否已初始化
        if (!gpTwainApplicationCMD) {
            Logger::Log("@WARN zhx_EndScan - The TWAIN environment is not initialized and does not need to be uninstalled");
            printf("[DLL WARN] The TWAIN environment is not initialized and does not need to be uninstalled\n");
            Logger::Cleanup();
            return;
        }
        
        // 检查当前状态
        if (gpTwainApplicationCMD->m_DSMState >= 5) {
            // 如果扫描仪处于已启用状态，先禁用它
            Logger::Log("@INFO The scanner is enabled, disable it first");
            printf("[DLL INFO] The scanner is enabled, disable it first\n");
            gpTwainApplicationCMD->disableDS();
        }
        
        // 检查是否有源可卸载
        if (gpTwainApplicationCMD->m_DSMState >= 4) {
            // 卸载数据源
            Logger::Log("@INFO Uninstall the data source");
            printf("[DLL INFO] Uninstall the data source\n");
            gpTwainApplicationCMD->unloadDS();
            
            // 验证卸载结果
            if (gpTwainApplicationCMD->m_DSMState == 3) {
                Logger::Log("@INFO The data source was successfully unmounted");
                printf("[DLL INFO] The data source was successfully unmounted\n");
            } else {
                Logger::Log("@WARN The data source is successfully uninstalled, and the status of the data source is abnormal after it is unmounted: %d", gpTwainApplicationCMD->m_DSMState);
                printf("[DLL WARN] The data source is successfully uninstalled, and the status of the data source is abnormal after it is unmounted: %d\n", gpTwainApplicationCMD->m_DSMState);
            }
        } else {
            Logger::Log("@INFO Currently, no data source needs to be uninstalled: %d", gpTwainApplicationCMD->m_DSMState);
            printf("[DLL INFO] Currently, no data source needs to be uninstalled: %d\n", gpTwainApplicationCMD->m_DSMState);
        }
    }
    catch (std::exception& e) {
        Logger::Log("@ERROR zhx_EndScan An exception has occurred: %s", e.what());
        printf("[DLL ERROR] An exception occurred while ending the scan: %s\n", e.what());
    }
    catch (...) {
        Logger::Log("@ERROR zhx_EndScan An unknown anomaly has occurred");
        printf("[DLL ERROR] An unknown exception occurred while ending the scan\n");
    }
    Logger::Cleanup();
}

/**
 * @brief 关闭当前连接的TWAIN环境并断开与DSM的连接
 * 
 * 此函数执行完整的TWAIN清理过程，包括：
 * 1. 禁用扫描仪（如果已启用）
 * 2. 卸载数据源（如果已加载）
 * 3. 断开与DSM的连接
 * 
 * 但不会删除TwainApp实例，如果需要完全清理，请使用zhx_Exit()。
 * 
 * @return 无返回值
 */
void zhx_CloseDevice() {
    Logger::Init();
    Logger::Log("@INFO zhx_CloseDevice called");
    printf("[DLL INFO] zhx_CloseDevice called\n");
    
    try {
        // 检查TWAIN环境是否已初始化
        if (!gpTwainApplicationCMD) {
            Logger::Log("@WARN zhx_CloseDevice - The TWAIN environment is not initialized and does not need to be disconnected");
            printf("[DLL WARN] The WAIN environment is not initialized and does not need to be disconnected\n");
            Logger::Cleanup();
            return;
        }
        
        // 检查当前状态并执行适当的清理
        if (gpTwainApplicationCMD->m_DSMState >= 5) {
            // 如果扫描仪处于已启用状态，先禁用它
            Logger::Log("@INFO The WAIN environment is not initialized and there is no need to disconnect the scanner when it is enabled, disable it first");
            printf("[DLL INFO] The WAIN environment is not initialized and there is no need to disconnect the scanner when it is enabled, disable it first\n");
            gpTwainApplicationCMD->disableDS();
        }
        
        if (gpTwainApplicationCMD->m_DSMState >= 4) {
            // 如果数据源已加载，先卸载它
            Logger::Log("@INFO The data source is loaded, uninstall it first");
            printf("[DLL INFO] The data source is loaded, uninstall it first\n");
            gpTwainApplicationCMD->unloadDS();
        }
        
        if (gpTwainApplicationCMD->m_DSMState >= 3) {
            // 断开与DSM的连接
            int prevState = gpTwainApplicationCMD->m_DSMState;
            Logger::Log("@INFO Disconnect from DSM, current state: %d", prevState);
            printf("[DLL INFO] Disconnect from DSM, current state: %d\n", prevState);
            
            gpTwainApplicationCMD->disconnectDSM();
            
            // 验证断开连接的结果
            if (gpTwainApplicationCMD->m_DSMState < 3) {
                Logger::Log("@INFO Successfully disconnected from DSM, new status: %d", gpTwainApplicationCMD->m_DSMState);
                printf("[DLL INFO] Successfully disconnected from DSM, new status: %d\n", gpTwainApplicationCMD->m_DSMState);
            } else {
                Logger::Log("@WARN The connection to the DSM is successfully disconnected, and the status is abnormal after the DSM connection is disconnected in the new state: %d", gpTwainApplicationCMD->m_DSMState);
                printf("[DLL WARN] The connection to the DSM is successfully disconnected, and the status is abnormal after the DSM connection is disconnected in the new state: %d\n", gpTwainApplicationCMD->m_DSMState);
            }
        } else {
            Logger::Log("@INFO Not currently connected to DSM, status: %d", gpTwainApplicationCMD->m_DSMState);
            printf("[DLL INFO] Not currently connected to DSM, status: %d\n", gpTwainApplicationCMD->m_DSMState);
        }
    }
    catch (std::exception& e) {
        Logger::Log("@ERROR zhx_CloseDevice An exception has occurred: %s", e.what());
        printf("[DLL ERROR] An exception occurred while turning off the device: %s\n", e.what());
    }
    catch (...) {
        Logger::Log("@ERROR zhx_CloseDevice An exception has occurredBBB");
        printf("[DLL ERROR] An exception occurred while turning off the deviceBBB\n");
    }
    Logger::Cleanup();
}

/**
 * @brief 完全退出TWAIN环境，释放所有资源
 * 
 * 此函数执行最终的清理工作，应在应用程序退出前调用：
 * 1. 禁用所有已启用的数据源
 * 2. 卸载所有已加载的数据源
 * 3. 断开与DSM的连接
 * 4. 调用TwainApp的exit()方法
 * 5. 删除TwainApp实例并释放内存
 * 
 * 调用此函数后，必须再次调用zhx_Init()才能使用TWAIN功能。
 * 
 * @return 无返回值
 */
void zhx_Exit() {
    Logger::Init();
    Logger::Log("@INFO zhx_Exit called");
    printf("[DLL INFO] zhx_Exit called\n");
    
    try {
        // 检查TWAIN环境是否已初始化
        if (!gpTwainApplicationCMD) {
            Logger::Log("@WARN zhx_Exit - The TWAIN environment is not initialized or cleaned up");
            printf("[DLL WARN] The TWAIN environment is not initialized or cleaned up\n");
            Logger::Cleanup();
            return;
        }
        
        // 记录当前状态
        int currentState = gpTwainApplicationCMD->m_DSMState;
        Logger::Log("@INFO Started cleaning up the TWAIN environment, as it is: %d", currentState);
        printf("[DLL INFO] Started cleaning up the TWAIN environment, as it is: %d\n", currentState);
        
        // 按TWAIN状态顺序执行完整清理
        if (currentState >= 5) {
            // 先禁用数据源
            Logger::Log("@INFO Disable the data source");
            printf("[DLL INFO] Disable the data source\n");
            gpTwainApplicationCMD->disableDS();
        }
        
        if (gpTwainApplicationCMD->m_DSMState >= 4) {
            // 卸载数据源
            Logger::Log("@INFO Uninstall the data source");
            printf("[DLL INFO] Uninstall the data source\n");
            gpTwainApplicationCMD->unloadDS();
        }
        
        if (gpTwainApplicationCMD->m_DSMState >= 3) {
            // 断开DSM连接
            Logger::Log("@INFO Disconnect the DSM");
            printf("[DLL INFO] Disconnect the DSM\n");
            gpTwainApplicationCMD->disconnectDSM();
        }
        
        // 调用TwainApp的exit方法进行最终清理
        Logger::Log("@INFO Call the exit method to release resources");
        printf("[DLL INFO] Call the exit method to release resources\n");
        gpTwainApplicationCMD->exit();
        
        // 释放TwainApp对象
        Logger::Log("@INFO Delete the TwainApp instance");
        printf("[DLL INFO] Delete the TwainApp instance\n");
        delete gpTwainApplicationCMD;
        gpTwainApplicationCMD = NULL; // 使用NULL而不是0，更符合C++风格
        
        Logger::Log("@INFO The TWAIN environment is cleaned up");
        printf("[DLL INFO] The TWAIN environment is cleaned up\n");
    }
    catch (std::exception& e) {
        Logger::Log("@ERROR zhx_Exit An exception has occurred: %s", e.what());
        printf("[DLL ERROR] An exception occurred while exiting the TWAIN environment: %s\n", e.what());
        
        // 即使发生异常，也尝试释放资源
        if (gpTwainApplicationCMD) {
            try {
                delete gpTwainApplicationCMD;
                gpTwainApplicationCMD = NULL;
                Logger::Log("@INFO An exception has occurred, the TwainApp resource has been released");
                printf("[DLL INFO] An exception has occurred, the TwainApp resource has been released\n");
            }
            catch (...) {
                Logger::Log("@ERROR An exception occurred while releasing the TwainApp resource");
                printf("[DLL ERROR] An exception occurred while releasing the TwainApp resource\n");
            }
        }
    }
    catch (...) {
        Logger::Log("@ERROR zhx_Exit An unknown anomaly occurred");
        printf("[DLL ERROR] An unknown anomaly occurred while exiting the TWAIN environment\n");
        
        // 同样尝试释放资源
        if (gpTwainApplicationCMD) {
            try {
                delete gpTwainApplicationCMD;
                gpTwainApplicationCMD = NULL;
                Logger::Log("@INFO An unknown anomaly occurred, the TwainApp resource has been released");
                printf("[DLL INFO] An unknown anomaly occurred, the TwainApp resource has been released\n");
            }
            catch (...) {
                Logger::Log("@ERROR An exception occurred while releasing the TwainApp resource");
                printf("[DLL ERROR] An exception occurred while releasing the TwainApp resource\n");
            }
        }
    }
    
    // 在完全退出前，记录最终状态
    Logger::Log("@INFO zhx_Exit completed, the TWAIN environment has been exited");
    printf("[DLL INFO] zhx_Exit completed, the TWAIN environment has been exited\n");
    Logger::Cleanup();
}


/**
 * TWSX_NATIVE (0) - 原生传输模式
 * TWSX_FILE (1) - 文件传输模式
 * TWSX_MEMORY (2) - 内存缓冲传输模式
 * TWSX_MEMFILE (4) - 内存文件传输模式
 * 设置扫描模式
 */
int zhx_SetTransferMechanism(int mechanism) {
    Logger::Init();
    Logger::Log("@INFO Setting transfer mechanism to %d", mechanism);
    
    // 检查TWAIN环境是否初始化
    if (!gpTwainApplicationCMD) {
        Logger::Log("@ERROR The TWAIN environment is not initialized");
        Logger::Cleanup();
        return 0;
    }
    
    // 检查传入的机制值是否有效
    if (mechanism != TWSX_NATIVE && 
        mechanism != TWSX_FILE && 
        mechanism != TWSX_MEMORY) {
        Logger::Log("@ERROR Invalid transfer mechanism: %d", mechanism);
        Logger::Cleanup();
        return 0;
    }
    
    // 设置传输机制
    gpTwainApplicationCMD->set_ICAP_XFERMECH((TW_UINT16)mechanism);
    Logger::Log("@INFO Transfer mechanism set successfully");
    Logger::Cleanup();
    return 1;
}

/**
 * 设置文件格式
 * 
 * 此函数用于设置扫描仪的文件格式。
 * 
 * @param format 文件格式，可以是以下值之一：
 * TWFF_TIFF (0) - TIFF单页格式
 * TWFF_PICT (1) - Macintosh PICT格式
 * TWFF_BMP (2) - Windows位图格式
 * TWFF_XBM (3) - X Windows位图格式
 * TWFF_JFIF (4) - JPEG图像格式
 * TWFF_FPX (5) - FlashPix格式
 * TWFF_TIFFMULTI (6) - 多页TIFF格式
 * TWFF_PNG (7) - PNG图像格式
 * TWFF_SPIFF (8) - SPIFF图像格式
 * TWFF_EXIF (9) - EXIF图像格式
 * TWFF_PDF (10) - PDF文档格式
 * TWFF_JP2 (11) - JPEG 2000格式
 * TWFF_JPX (13) - JPX格式(JPEG 2000扩展)
 * TWFF_DEJAVU (14) - DejaVu格式
 * TWFF_PDFA (15) - PDF/A格式(用于归档)
 * TWFF_PDFA2 (16) - PDF/A-2格式
 * TWFF_PDFRASTER (17) - PDF/Raster格式
 */
// 创建一个设置文件格式的函数
int zhx_SetImageFileFormat(int format) {
    Logger::Init();
    Logger::Log("@INFO start set image file format: %d", format);
    
    if (!gpTwainApplicationCMD) {
        Logger::Log("@ERROR TWAIN not initialized");
        Logger::Cleanup();
        return 0;
    }
    
    // 首先检查支持的格式
    checkSupportedFormats();
    
    // 设置为文件传输模式
    gpTwainApplicationCMD->set_ICAP_XFERMECH(TWSX_FILE);
    Logger::Log("@INFO set transfer mode to file mode");
    
    // 设置文件格式
    Logger::Log("@INFO try to set image file format to: %d", format);
    gpTwainApplicationCMD->set_ICAP_IMAGEFILEFORMAT((TW_UINT16)format);
    
    // 验证设置
    TW_UINT16 currentFormat;
    if (gpTwainApplicationCMD->getICAP_IMAGEFILEFORMAT(currentFormat)) {
        if (currentFormat == format) {
            Logger::Log("@INFO successfully set image file format to: %d", format);
            return 1;
        } else {
            Logger::Log("@WARNING request to set format to %d, but the scanner selected format %d", 
                       format, currentFormat);
            
            // 决定是接受扫描仪的选择还是尝试其他格式
            // 如果您要严格要求特定格式，可以在这里处理
            
            // 返回0表示未设置为请求的格式
            return 0;
        }
    } else {
        Logger::Log("@ERROR failed to get current format setting");
        return 0;
    }
    Logger::Cleanup();
}


/**
 * Check and log all image file formats supported by the current scanner
 * 
 * This function queries the scanner for supported image file formats,
 * logs them to the application log, and specifically checks for PNG support.
 * It's useful for diagnostic purposes and to verify scanner capabilities
 * before attempting to set specific file formats.
 */
void checkSupportedFormats() {
    Logger::Log("@INFO check supported formats of scanner...");
    
    // Create capability structure for querying file formats
    TW_CAPABILITY cap;
    cap.Cap = ICAP_IMAGEFILEFORMAT;   // Capability to query image file formats
    cap.ConType = TWON_DONTCARE16;    // Let the scanner choose the container type
    cap.hContainer = NULL;            // Container will be allocated by DSM_Entry

    // Query the scanner for supported file formats
    TW_UINT16 rc = gpTwainApplicationCMD->DSM_Entry(
        DG_CONTROL, DAT_CAPABILITY, MSG_GET, (TW_MEMREF)&cap);

    if (rc == TWRC_SUCCESS) {
        // Lock the memory to access the container data
        pTW_ENUMERATION pValues = (pTW_ENUMERATION)_DSM_LockMemory(cap.hContainer);
        if (pValues) {
            Logger::Log("@INFO scanner supports %d file formats:", pValues->NumItems);
            
            // Loop through each supported format and log it
            for (TW_UINT32 i = 0; i < pValues->NumItems; i++) {
                TW_UINT16 format = ((pTW_UINT16)(&pValues->ItemList))[i];
                const char* ext = convertICAP_IMAGEFILEFORMAT_toExt(format);
                Logger::Log("@INFO    format %d: %s", format, ext);
            }
            
            // Get and log the current default format
            TW_UINT16 currentFormat = ((pTW_UINT16)(&pValues->ItemList))[pValues->CurrentIndex];
            Logger::Log("@INFO current default format: %d (%s)", 
                       currentFormat, 
                       convertICAP_IMAGEFILEFORMAT_toExt(currentFormat));
            
            // Specifically check if PNG format is supported
            // This is important for applications that prefer PNG format
            bool supportsPNG = false;
            for (TW_UINT32 i = 0; i < pValues->NumItems; i++) {
                if (((pTW_UINT16)(&pValues->ItemList))[i] == TWFF_PNG) {
                    supportsPNG = true;
                    break;
                }
            }
            
            // Log PNG support status
            if (supportsPNG) {
                Logger::Log("@INFO scanner supports PNG format");
            } else {
                Logger::Log("@WARNING scanner does not support PNG format!");
            }
            
            // Unlock the memory when done accessing the container data
            _DSM_UnlockMemory(cap.hContainer);
        }
        // Free the memory allocated by DSM_Entry
        _DSM_Free(cap.hContainer);
    } else {
        // Log error if query failed
        Logger::Log("@ERROR failed to query scanner supported formats, error code: %d", rc);
    }
}

/**
 * 获取当前选择的文件格式
 * 
 * @return 当前的文件格式代码，出错返回-1
 */
int zhx_GetCurrentFileFormat()
{
    Logger::Init();
    // 检查全局应用程序实例和状态
    if (!gpTwainApplicationCMD || gpTwainApplicationCMD->m_DSMState < 4)
    {
        Logger::Log("@ERROR 获取当前文件格式失败：数据源未打开");
        return -1;
    }
    
    TW_UINT16 currentFormat;
    if (gpTwainApplicationCMD->getICAP_IMAGEFILEFORMAT(currentFormat))
    {
        // 记录当前格式
        const char* formatName = "未知";
        switch (currentFormat)
        {
            case TWFF_TIFF: formatName = "TIFF"; break;
            case TWFF_PICT: formatName = "PICT"; break;
            case TWFF_BMP: formatName = "BMP"; break;
            case TWFF_XBM: formatName = "XBM"; break;
            case TWFF_JFIF: formatName = "JPEG"; break;
            case TWFF_FPX: formatName = "FlashPix"; break;
            case TWFF_TIFFMULTI: formatName = "TIFFMULTI"; break;
            case TWFF_PNG: formatName = "PNG"; break;
            case TWFF_SPIFF: formatName = "SPIFF"; break;
            case TWFF_EXIF: formatName = "EXIF"; break;
            case TWFF_PDF: formatName = "PDF"; break;
            case TWFF_JP2: formatName = "JP2"; break;
            case TWFF_JPN: formatName = "JPN"; break;
            case TWFF_JPX: formatName = "JPX"; break;
            case TWFF_DEJAVU: formatName = "DEJAVU"; break;
            case TWFF_PDFA: formatName = "PDFA"; break;
            case TWFF_PDFA2: formatName = "PDFA2"; break;
        }
        
        Logger::Log("@INFO 当前文件格式为 %s (%d)", formatName, currentFormat);
        return currentFormat;
    }
    
    Logger::Log("@ERROR 获取当前文件格式失败");
    Logger::Cleanup();
    return -1;
}


/**
 * 获取扫描仪支持的文件格式列表
 * 
 * @return 返回包含所有支持格式的JSON字符串，格式为"[{\"label\":\"格式1\",\"value\":1},{\"label\":\"格式2\",\"value\":2}]"，如果出错则返回空JSON数组"[]"
 */
char *zhx_GetSupportedFileFormats()
{
    static char result[1024] = {0}; // 静态缓冲区存储结果字符串
    memset(result, 0, sizeof(result));
    strcpy(result, "[]"); // 默认为空JSON数组
    Logger::Init();
    // 检查全局应用程序实例和状态
    if (!gpTwainApplicationCMD || gpTwainApplicationCMD->m_DSMState < 4)
    {
        Logger::Log("@ERROR Get file format failed: The data source is not open");
        return result;
    }
    
    // 创建能力结构
    TW_CAPABILITY cap;
    memset(&cap, 0, sizeof(TW_CAPABILITY));
    cap.Cap = ICAP_IMAGEFILEFORMAT;
    cap.ConType = TWON_DONTCARE16;
    
    // 获取文件格式能力
    TW_UINT16 rc = gpTwainApplicationCMD->DSM_Entry(
        DG_CONTROL, DAT_CAPABILITY, MSG_GET, (TW_MEMREF)&cap);
    
    if (rc != TWRC_SUCCESS)
    {
        Logger::Log("@ERROR Get file format support failed: TWAIN error");
        return result;
    }
    
    // 检查返回的容器类型
    if (cap.ConType != TWON_ENUMERATION)
    {
        if (cap.hContainer)
        {
            _DSM_Free(cap.hContainer);
        }
        Logger::Log("@ERROR Get file format support failed: The returned container type is not an enumeration type");
        return result;
    }
    
    // 访问容器数据
    pTW_ENUMERATION pEnum = (pTW_ENUMERATION)_DSM_LockMemory(cap.hContainer);
    
    if (!pEnum)
    {
        _DSM_Free(cap.hContainer);
        Logger::Log("@ERROR Get file format support failed: Unable to lock memory");
        return result;
    }
    
    // 初始化JSON数组
    strcpy(result, "[");
    int offset = 1; // 起始位置，因为已经写入了 "["
    
    // 添加支持的格式信息
    for (TW_UINT16 i = 0; i < pEnum->NumItems; i++)
    {
        TW_UINT16 format = ((TW_UINT16*)(pEnum->ItemList))[i];
        const char* formatName = "";
        
        // 转换格式代码为可读字符串
        switch (format)
        {
            case TWFF_TIFF:
                formatName = "TIFF";
                break;
            case TWFF_PICT:
                formatName = "PICT";
                break;
            case TWFF_BMP:
                formatName = "BMP";
                break;
            case TWFF_XBM:
                formatName = "XBM";
                break;
            case TWFF_JFIF:
                formatName = "JPEG";
                break;
            case TWFF_FPX:
                formatName = "FlashPix";
                break;
            case TWFF_TIFFMULTI:
                formatName = "TIFFMULTI";
                break;
            case TWFF_PNG:
                formatName = "PNG";
                break;
            case TWFF_SPIFF:
                formatName = "SPIFF";
                break;
            case TWFF_EXIF:
                formatName = "EXIF";
                break;
            case TWFF_PDF:
                formatName = "PDF";
                break;
            case TWFF_JP2:
                formatName = "JP2";
                break;
            case TWFF_JPN:
                formatName = "JPN";
                break;
            case TWFF_JPX:
                formatName = "JPX";
                break;
            case TWFF_DEJAVU:
                formatName = "DEJAVU";
                break;
            case TWFF_PDFA:
                formatName = "PDFA";
                break;
            case TWFF_PDFA2:
                formatName = "PDFA2";
                break;
            default:
                formatName = "UNKNOWN";
                break;
        }
        
        // 添加JSON对象到结果数组，使用逗号分隔
        if (i > 0)
        {
            offset += sprintf(result + offset, ",");
        }
        
        // 添加键值对形式的对象
        offset += sprintf(result + offset, "{\"label\":\"%s\",\"value\":%d}", formatName, format);
        
        // 防止缓冲区溢出
        if (offset >= sizeof(result) - 50)  // 留出足够空间给下一个格式和结束括号
        {
            break;
        }
    }
    
    // 添加结束括号
    strcat(result, "]");
    
    // 解锁并释放内存
    _DSM_UnlockMemory(cap.hContainer);
    _DSM_Free(cap.hContainer);
    
    Logger::Log("@INFO Supported file formatsJSON: %s", result);
    Logger::Cleanup();
    return result;
}


/**
 * 设置扫描分辨率(DPI)
 * 
 * @param dpi 要设置的DPI值，典型值为：75, 100, 150, 200, 300, 600等
 * @return 成功返回1，失败返回0
 */
int zhx_SetResolution(int dpi) {
    Logger::Init();
    Logger::Log("@INFO Starting to set resolution to: %d DPI", dpi);
    
    // Check if TWAIN environment is initialized
    if (!gpTwainApplicationCMD) {
        Logger::Log("@ERROR TWAIN environment not initialized");
        Logger::Cleanup();
        return 0;
    }
    
    // Check if scanner is open
    if (gpTwainApplicationCMD->m_DSMState < 4) {
        Logger::Log("@ERROR Not connected to scanning device, current state: %d", gpTwainApplicationCMD->m_DSMState);
        Logger::Cleanup();
        return 0;
    }
    
    // Create TW_FIX32 structure to represent DPI value
    TW_FIX32 resolution;
    // Convert integer DPI to TW_FIX32 format
    // TW_FIX32 is a 32-bit fixed-point number, consisting of 16-bit integer part and 16-bit fractional part
    resolution.Whole = dpi;  // Integer part
    resolution.Frac = 0;     // Fractional part is 0
    
    // Set X resolution
    gpTwainApplicationCMD->set_ICAP_RESOLUTION(ICAP_XRESOLUTION, &resolution);
    
    // Set Y resolution
    gpTwainApplicationCMD->set_ICAP_RESOLUTION(ICAP_YRESOLUTION, &resolution);
    
    // Verify if setting was successful
    TW_FIX32 currentXRes, currentYRes;
    bool success = gpTwainApplicationCMD->getICAP_XRESOLUTION(currentXRes) && 
                  gpTwainApplicationCMD->getICAP_YRESOLUTION(currentYRes);
                  
    if (success && currentXRes.Whole == dpi && currentYRes.Whole == dpi) {
        Logger::Log("@INFO Resolution successfully set to %d DPI", dpi);
        Logger::Cleanup();
        return 1;
    } else {
        if (success) {
            Logger::Log("@WARN Requested to set DPI to %d, but scanner selected X:%d.%04d, Y:%d.%04d", 
                       dpi, currentXRes.Whole, currentXRes.Frac, currentYRes.Whole, currentYRes.Frac);
        } else {
            Logger::Log("@ERROR Failed to get current resolution settings");
        }
        Logger::Cleanup();
        return 0;
    }
}


/**
 * Get the current DPI (resolution) setting of the scanner
 * 
 * @return Returns the current DPI value as an integer, or -1 on error
 */
int zhx_GetCurrentResolution() {
    Logger::Init();
    Logger::Log("@INFO Getting current scanner resolution");
    
    // Check if TWAIN environment is initialized
    if (!gpTwainApplicationCMD) {
        Logger::Log("@ERROR TWAIN environment not initialized");
        Logger::Cleanup();
        return -1;
    }
    
    // Check if scanner is open
    if (gpTwainApplicationCMD->m_DSMState < 4) {
        Logger::Log("@ERROR Not connected to scanning device, current state: %d", gpTwainApplicationCMD->m_DSMState);
        Logger::Cleanup();
        return -1;
    }
    
    // Get current X resolution
    TW_FIX32 xRes;
    if (!gpTwainApplicationCMD->getICAP_XRESOLUTION(xRes)) {
        Logger::Log("@ERROR Failed to get current X resolution");
        Logger::Cleanup();
        return -1;
    }
    
    // Get current Y resolution (for verification)
    TW_FIX32 yRes;
    if (!gpTwainApplicationCMD->getICAP_YRESOLUTION(yRes)) {
        Logger::Log("@ERROR Failed to get current Y resolution");
        Logger::Cleanup();
        return -1;
    }
    
    // Convert FIX32 to integer
    int xDPI = xRes.Whole;
    int yDPI = yRes.Whole;
    
    // Log the result
    if (xDPI == yDPI) {
        Logger::Log("@INFO Current scanner resolution is %d DPI", xDPI);
    } else {
        Logger::Log("@INFO Current scanner resolution is X: %d DPI, Y: %d DPI (different X/Y resolutions)", xDPI, yDPI);
    }
    
    Logger::Cleanup();
    return xDPI; // Return X resolution as the primary value
}

/**
 * Get all DPI values supported by the scanner
 * 
 * @return Returns a JSON string with format "[{\"label\":\"75 DPI\",\"value\":75},{\"label\":\"300 DPI\",\"value\":300},...]"
 *         or empty array "[]" on error
 */
char* zhx_GetSupportedResolutions() {
    static char result[1024] = {0}; // Static buffer to store the result string
    memset(result, 0, sizeof(result));
    strcpy(result, "[]"); // Default to empty JSON array
    
    Logger::Init();
    Logger::Log("@INFO Retrieving supported scanner resolutions");
    
    // Check if TWAIN environment is initialized
    if (!gpTwainApplicationCMD) {
        Logger::Log("@ERROR TWAIN environment not initialized");
        Logger::Cleanup();
        return result;
    }
    
    // Check if scanner is open
    if (gpTwainApplicationCMD->m_DSMState < 4) {
        Logger::Log("@ERROR Not connected to scanning device, current state: %d", gpTwainApplicationCMD->m_DSMState);
        Logger::Cleanup();
        return result;
    }
    
    // Create capability structure
    TW_CAPABILITY cap;
    memset(&cap, 0, sizeof(TW_CAPABILITY));
    cap.Cap = ICAP_XRESOLUTION; // We use X resolution (vertical resolution is typically the same)
    cap.ConType = TWON_DONTCARE16;
    
    // Get resolution capability
    TW_UINT16 rc = gpTwainApplicationCMD->DSM_Entry(
        DG_CONTROL, DAT_CAPABILITY, MSG_GET, (TW_MEMREF)&cap);
    
    if (rc != TWRC_SUCCESS) {
        Logger::Log("@ERROR Failed to get resolution support: TWAIN error");
        return result;
    }
    
    // Check returned container type
    if (cap.ConType != TWON_ENUMERATION) {
        if (cap.hContainer) {
            _DSM_Free(cap.hContainer);
        }
        Logger::Log("@ERROR Failed to get resolution support: Return container type is not enumeration type");
        return result;
    }
    
    // Access container data
    pTW_ENUMERATION_FIX32 pEnum = (pTW_ENUMERATION_FIX32)_DSM_LockMemory(cap.hContainer);
    
    if (!pEnum) {
        _DSM_Free(cap.hContainer);
        Logger::Log("@ERROR Failed to get resolution support: Unable to lock memory");
        return result;
    }
    
    // Initialize JSON array
    strcpy(result, "[");
    int offset = 1; // Starting position, since we've already written "["
    
    // Add supported resolution information
    for (TW_UINT16 i = 0; i < pEnum->NumItems; i++) {
        TW_FIX32 resolution = pEnum->ItemList[i];
        
        // Convert FIX32 to float
        float dpi = (float)resolution.Whole + ((float)resolution.Frac / 65536.0f);
        
        // Add JSON object to result array, use comma as delimiter
        if (i > 0) {
            offset += sprintf(result + offset, ",");
        }
        
        // Add key-value pair object with "DPI" added to the label
        offset += sprintf(result + offset, "{\"label\":\"%d DPI\",\"value\":%d}", 
                         (int)dpi, (int)dpi);
        
        // Prevent buffer overflow
        if (offset >= sizeof(result) - 50) {  // Leave enough space for the next format and closing bracket
            break;
        }
    }
    
    // Add closing bracket
    strcat(result, "]");
    
    // Unlock and free memory
    _DSM_UnlockMemory(cap.hContainer);
    _DSM_Free(cap.hContainer);
    
    Logger::Log("@INFO Supported resolutions JSON: %s", result);
    Logger::Cleanup();
    return result;
}


/**
 * Get all capabilities supported by a specific scanner device
 * 
 * @param device Name of the scanner device to query
 * @return JSON string with capabilities in format "[{\"name\":\"CAP_XFERCOUNT\",\"value\":1,\"label\":\"Transfer Count\"},...]"
 */
char* zhx_GetDevCapability_JSON(char* device) {
    static char result[4096] = {0}; // Larger buffer for potentially many capabilities
    memset(result, 0, sizeof(result));
    strcpy(result, "[]"); // Default to empty JSON array
    
    Logger::Init();
    Logger::Log("@INFO Retrieving capabilities for scanner: %s", device);
    
    // Check if TWAIN environment is initialized
    if (!gpTwainApplicationCMD) {
        Logger::Log("@ERROR TWAIN environment not initialized");
        Logger::Cleanup();
        return result;
    }
    
    // Save current connection state
    bool wasConnected = false;
    int previousState = gpTwainApplicationCMD->m_DSMState;
    std::string previousDevice = "";
    
    // If already connected to a device, record which one and disconnect
    if (previousState >= 4) {
        wasConnected = true;
        previousDevice = gpTwainApplicationCMD->getSourceIdentity();
        
        // Close current device if it's different from the requested one
        if (previousDevice != device) {
            Logger::Log("@INFO Temporarily closing current device: %s", previousDevice.c_str());
            gpTwainApplicationCMD->disableDS();
            gpTwainApplicationCMD->unloadDS();
        } else {
            // If same device, we can skip reconnection
            Logger::Log("@INFO Already connected to requested device: %s", device);
        }
    }
    
    // Connect to the specified device if not already connected to it
    bool newConnectionNeeded = !wasConnected || previousDevice != device;
    bool connectionSuccess = true;
    
    if (newConnectionNeeded) {
        Logger::Log("@INFO Connecting to device: %s", device);
        
        // 修正: 使用zhx_GetDeviceNumber获取设备编号
        int deviceNumber = zhx_GetDeviceNumber(device);
        if (deviceNumber <= 0) {
            Logger::Log("@ERROR Failed to get device number for: %s", device);
            connectionSuccess = false;
        } else {
            // 加载数据源，使用设备编号而不是设备名称
            int prevState = gpTwainApplicationCMD->m_DSMState;
            gpTwainApplicationCMD->loadDS(deviceNumber);
            if (gpTwainApplicationCMD->m_DSMState != 4) { // 成功loadDS后状态应该是4
                Logger::Log("@ERROR Failed to load device: %s (number: %d)", device, deviceNumber);
                connectionSuccess = false;
            }
        }
    }
    
    // Proceed with capability query if connection is established
    if ((newConnectionNeeded && connectionSuccess) || (!newConnectionNeeded && previousState >= 4)) {
        // Create capability structure for CAP_SUPPORTEDCAPS
        TW_CAPABILITY cap;
        memset(&cap, 0, sizeof(TW_CAPABILITY));
        cap.Cap = CAP_SUPPORTEDCAPS;
        cap.ConType = TWON_DONTCARE16;
        
        // Query the scanner for supported capabilities
        TW_UINT16 rc = gpTwainApplicationCMD->DSM_Entry(
            DG_CONTROL, DAT_CAPABILITY, MSG_GET, (TW_MEMREF)&cap);
        
        if (rc != TWRC_SUCCESS) {
            Logger::Log("@ERROR Failed to get supported capabilities: TWAIN error %d", rc);
        } else {
            // Check returned container type
            if (cap.ConType != TWON_ARRAY) {
                if (cap.hContainer) {
                    _DSM_Free(cap.hContainer);
                }
                Logger::Log("@ERROR Failed to get capabilities: Return container type is not ARRAY");
            } else {
                // Access container data
                pTW_ARRAY_UINT16 pArray = (pTW_ARRAY_UINT16)_DSM_LockMemory(cap.hContainer);
                
                if (!pArray) {
                    _DSM_Free(cap.hContainer);
                    Logger::Log("@ERROR Failed to get capabilities: Unable to lock memory");
                } else {
                    // Initialize JSON array
                    strcpy(result, "[");
                    int offset = 1; // Starting position, since we've already written "["
                    
                    // Process each capability
                    for (TW_UINT32 i = 0; i < pArray->NumItems && offset < sizeof(result) - 256; i++) {
                        TW_UINT16 capValue = pArray->ItemList[i];
                        const char* capName = convertCAP_toString(capValue);
                        const char* capLabel = getCapabilityChineseLabel(capValue); // Using English labels
                        
                        // Add JSON object to result array, use comma as delimiter
                        if (i > 0) {
                            offset += sprintf(result + offset, ",");
                        }
                        
                        // Add key-value-label triple in JSON format
                        offset += sprintf(result + offset, "{\"name\":\"%s\",\"value\":%d,\"label\":\"%s\"}", 
                                         capName, capValue, capLabel);
                    }
                    
                    // Add closing bracket
                    strcat(result, "]");
                    
                    // Unlock and free memory
                    _DSM_UnlockMemory(cap.hContainer);
                    
                    Logger::Log("@INFO Retrieved %d supported capabilities for device: %s", pArray->NumItems, device);
                }
                
                _DSM_Free(cap.hContainer);
            }
        }
    }
    
    // Restore previous connection if needed
    if (newConnectionNeeded) {
        // Close the temporary device connection
        if (connectionSuccess) {
            gpTwainApplicationCMD->disableDS();
            gpTwainApplicationCMD->unloadDS();
        }
        
        // 恢复之前设备连接的部分
        if (wasConnected && previousDevice != device) {
            Logger::Log("@INFO Restoring connection to previous device: %s", previousDevice.c_str());
            
            // 修正: 使用zhx_GetDeviceNumber获取之前设备的编号
            int prevDeviceNumber = zhx_GetDeviceNumber(previousDevice.c_str());
            if (prevDeviceNumber > 0) {
                int prevState = gpTwainApplicationCMD->m_DSMState;
                gpTwainApplicationCMD->loadDS(prevDeviceNumber);
                if (gpTwainApplicationCMD->m_DSMState == 4) {
                    Logger::Log("@INFO Successfully restored previous device connection");
                } else {
                    Logger::Log("@WARNING Failed to reload previous device: %s (number: %d)", previousDevice.c_str(), prevDeviceNumber);
                }
            } else {
                Logger::Log("@WARNING Failed to get device number for previous device: %s", previousDevice.c_str());
            }
        }
    }
    
    Logger::Cleanup();
    return result;
}



/**
 * @brief 获取指定设备支持的全部TWAIN能力列表
 * 
 * 此函数连接到指定的TWAIN设备并查询其支持的所有能力(capabilities)。
 * 返回结果格式为："支持的capabilities: CAP_NAME1:VALUE1;CAP_NAME2:VALUE2;..."
 * 如果设备已经连接，会临时保存连接状态，查询完后恢复原有连接。
 * 
 * @param device 要查询的设备名称
 * @return 返回包含设备所有支持能力的字符串
 */
char* zhx_GetDevCapability_STR(char* device) {
    // 为返回结果分配静态缓冲区，确保跨函数调用有效
    static char result[4096] = {0}; // 保持较大的缓冲区
    memset(result, 0, sizeof(result));
    strcpy(result, "支持的capabilities: "); // 初始化结果字符串
    int offset = strlen(result); // 当前写入位置
    
    // 初始化日志记录器
    Logger::Init();
    Logger::Log("@INFO Retrieving capabilities for scanner: %s", device);
    
    // 检查TWAIN环境是否初始化
    if (!gpTwainApplicationCMD) {
        Logger::Log("@ERROR TWAIN environment not initialized");
        Logger::Cleanup();
        return result; // 返回空结果
    }
    
    // 保存当前连接状态，以便在完成后恢复
    bool wasConnected = false;
    int previousState = gpTwainApplicationCMD->m_DSMState;
    std::string previousDevice = "";
    
    // 检查是否已连接到其他设备，如果是则记录设备信息
    if (previousState >= 4) {
        wasConnected = true;
        previousDevice = gpTwainApplicationCMD->getSourceIdentity();
        
        // 如果当前连接的设备与要查询的设备不同，则暂时关闭当前设备
        if (previousDevice != device) {
            Logger::Log("@INFO Temporarily closing current device: %s", previousDevice.c_str());
            gpTwainApplicationCMD->disableDS();
            gpTwainApplicationCMD->unloadDS();
        } else {
            // 如果已连接到请求的设备，则无需重新连接
            Logger::Log("@INFO Already connected to requested device: %s", device);
        }
    }
    
    // 确定是否需要建立新连接
    bool newConnectionNeeded = !wasConnected || previousDevice != device;
    bool connectionSuccess = true;
    
    // 如果需要新连接，则连接到指定设备
    if (newConnectionNeeded) {
        Logger::Log("@INFO Connecting to device: %s", device);
        
        // 使用设备名称获取设备编号
        int deviceNumber = zhx_GetDeviceNumber(device);
        if (deviceNumber <= 0) {
            Logger::Log("@ERROR Failed to get device number for: %s", device);
            connectionSuccess = false;
        } else {
            // 加载指定设备的数据源
            int prevState = gpTwainApplicationCMD->m_DSMState;
            gpTwainApplicationCMD->loadDS(deviceNumber);
            
            // 检查加载是否成功
            if (gpTwainApplicationCMD->m_DSMState != 4) {
                Logger::Log("@ERROR Failed to load device: %s (number: %d)", device, deviceNumber);
                connectionSuccess = false;
            }
        }
    }
    
    // 如果已成功连接到设备，继续查询其支持的能力
    if ((newConnectionNeeded && connectionSuccess) || (!newConnectionNeeded && previousState >= 4)) {
        // 创建查询CAP_SUPPORTEDCAPS的能力结构
        TW_CAPABILITY cap;
        memset(&cap, 0, sizeof(TW_CAPABILITY));
        cap.Cap = CAP_SUPPORTEDCAPS; // 查询所有支持的能力
        cap.ConType = TWON_DONTCARE16; // 不关心返回的容器类型
        
        // 调用TWAIN DSM接口查询支持的能力
        TW_UINT16 rc = gpTwainApplicationCMD->DSM_Entry(
            DG_CONTROL, DAT_CAPABILITY, MSG_GET, (TW_MEMREF)&cap);
        
        // 检查查询结果
        if (rc != TWRC_SUCCESS) {
            Logger::Log("@ERROR Failed to get supported capabilities: TWAIN error %d", rc);
        } else {
            // 确认返回的容器类型是数组
            if (cap.ConType != TWON_ARRAY) {
                if (cap.hContainer) {
                    _DSM_Free(cap.hContainer);
                }
                Logger::Log("@ERROR Failed to get capabilities: Return container type is not ARRAY");
            } else {
                // 锁定内存访问容器数据
                pTW_ARRAY_UINT16 pArray = (pTW_ARRAY_UINT16)_DSM_LockMemory(cap.hContainer);
                
                if (!pArray) {
                    _DSM_Free(cap.hContainer);
                    Logger::Log("@ERROR Failed to get capabilities: Unable to lock memory");
                } else {
                    // 遍历数组中的每个能力项，格式化为输出字符串
                    for (TW_UINT32 i = 0; i < pArray->NumItems && offset < sizeof(result) - 256; i++) {
                        TW_UINT16 capValue = pArray->ItemList[i]; // 获取能力ID
                        const char* capName = convertCAP_toString(capValue); // 转换ID为名称
                        
                        // 使用分号作为分隔符，第一个项目不需要分号
                        if (i > 0) {
                            offset += sprintf(result + offset, ";");
                        }
                        
                        // 格式化输出为 "CAP_NAME:VALUE" 格式
                        offset += sprintf(result + offset, "%s:%d", capName, capValue);
                    }
                    
                    // 解锁内存并记录日志
                    _DSM_UnlockMemory(cap.hContainer);
                    Logger::Log("@INFO Retrieved %d supported capabilities for device: %s", pArray->NumItems, device);
                }
                
                // 释放TWAIN分配的内存
                _DSM_Free(cap.hContainer);
            }
        }
    }
    
    // 恢复之前的设备连接（如果需要）
    if (newConnectionNeeded) {
        // 关闭临时设备连接
        if (connectionSuccess) {
            gpTwainApplicationCMD->disableDS();
            gpTwainApplicationCMD->unloadDS();
        }
        
        // 如果之前有连接且设备不同，则恢复到之前的设备
        if (wasConnected && previousDevice != device) {
            Logger::Log("@INFO Restoring connection to previous device: %s", previousDevice.c_str());
            
            // 获取之前设备的编号并重新连接
            int prevDeviceNumber = zhx_GetDeviceNumber(previousDevice.c_str());
            if (prevDeviceNumber > 0) {
                int prevState = gpTwainApplicationCMD->m_DSMState;
                gpTwainApplicationCMD->loadDS(prevDeviceNumber);
                
                // 检查恢复连接是否成功
                if (gpTwainApplicationCMD->m_DSMState == 4) {
                    Logger::Log("@INFO Successfully restored previous device connection");
                } else {
                    Logger::Log("@WARNING Failed to reload previous device: %s (number: %d)", 
                            previousDevice.c_str(), prevDeviceNumber);
                }
            } else {
                Logger::Log("@WARNING Failed to get device number for previous device: %s", 
                        previousDevice.c_str());
            }
        }
    }
    
    // 清理日志记录器
    Logger::Cleanup();
    return result; // 返回格式化后的结果字符串
}



/**
 * 获取能力ID对应的中文说明
 * 
 * @param capValue 能力ID值
 * @return 返回对应的中文说明
 */
const char* getCapabilityChineseLabel(TW_UINT16 capValue) {
    switch (capValue) {
        // 基本控制能力
        // Basic Control Capabilities
        case CAP_DEVICEONLINE:      return "Device Online Status";
        case CAP_INDICATORS:        return "Progress Indicators";
        case CAP_ENABLEDSUIONLY:    return "Enable DS UI Only";
        case CAP_UICONTROLLABLE:    return "UI Controllable";
        case CAP_SUPPORTEDCAPS:     return "Supported Capabilities";
        case CAP_CUSTOMINTERFACEGUID: return "Custom Interface GUID";
        case CAP_CUSTOMDSDATA:      return "Custom DS Data";
        
        // Feeder Related
        case CAP_PAPERDETECTABLE:   return "Paper Detection";
        case CAP_FEEDERENABLED:     return "Feeder Enabled";
        case CAP_FEEDERLOADED:      return "Feeder Loaded";
        case CAP_DUPLEX:            return "Duplex Support";
        case CAP_DUPLEXENABLED:     return "Duplex Enabled";
        case CAP_AUTOFEED:          return "Auto Feed";
        
        // Image Properties and Transfer
        case CAP_XFERCOUNT:         return "Transfer Count";
        case ICAP_BITDEPTH:         return "Bit Depth";
        case ICAP_BITORDER:         return "Bit Order";
        case ICAP_COMPRESSION:      return "Compression";
        case ICAP_IMAGEFILEFORMAT:  return "Image File Format";
        case ICAP_PIXELFLAVOR:      return "Pixel Flavor";
        case ICAP_PIXELTYPE:        return "Pixel Type";
        case ICAP_PLANARCHUNKY:     return "Planar/Chunky";
        case ICAP_XFERMECH:         return "Transfer Mechanism";
        
        // Page and Image Size
        case ICAP_FRAMES:           return "Image Frames";
        case ICAP_MAXFRAMES:        return "Maximum Frames";
        case ICAP_PHYSICALHEIGHT:   return "Physical Height";
        case ICAP_PHYSICALWIDTH:    return "Physical Width";
        case ICAP_SUPPORTEDSIZES:   return "Supported Paper Sizes";
        case ICAP_ORIENTATION:      return "Image Orientation";
        case ICAP_UNITS:            return "Measurement Units";
        
        // Resolution
        case ICAP_XNATIVERESOLUTION: return "Native X Resolution";
        case ICAP_YNATIVERESOLUTION: return "Native Y Resolution";
        case ICAP_XRESOLUTION:      return "X Resolution";
        case ICAP_YRESOLUTION:      return "Y Resolution";
        
        // Image Enhancement
        case ICAP_THRESHOLD:        return "Threshold";
        case ICAP_CONTRAST:         return "Contrast";
        case ICAP_BRIGHTNESS:       return "Brightness";
        case ICAP_GAMMA:            return "Gamma";
        
        // Custom Capabilities
        case 4158:                  return "Vendor Specific";
        case 32769:                 return "Custom Capability 1";
        case 32770:                 return "Custom Capability 2";
        
        default:
            if (capValue >= CAP_CUSTOMBASE) {
                return "Custom Capability";
            } else if (capValue >= ICAP_AUTOBRIGHT && capValue <= ICAP_ZOOMFACTOR) {
                return "Image Control Capability";
            } else {
                return "Basic Control Capability";
            }
    }
}



/**
 * @brief 获取指定能力的支持值
 * 
 * 此函数可以获取当前打开的数据源中特定能力项支持的值列表，
 * 或者如果传入的是扫描仪名称，则返回该扫描仪支持的所有能力。
 * 
 * @param capOrDevice 能力项名称(如"ICAP_SUPPORTEDSIZES")或扫描仪名称
 * @return 返回JSON格式的支持值列表
 */
char* zhx_GetCapability_STR(char* capOrDevice) {
    static char result[4096] = {0}; // 分配足够大的缓冲区
    memset(result, 0, sizeof(result));
    strcpy(result, "[]"); // 默认为空JSON数组
    
    Logger::Init();
    Logger::Log("@INFO Retrieving capability information for: %s", capOrDevice);
    
    // 检查TWAIN环境是否初始化
    if (!gpTwainApplicationCMD) {
        Logger::Log("@ERROR TWAIN environment not initialized");
        Logger::Cleanup();
        return result;
    }
    
    // 检查是否已连接到数据源
    if (gpTwainApplicationCMD->m_DSMState < 4) {
        Logger::Log("@ERROR Not connected to scanning device, current state: %d", gpTwainApplicationCMD->m_DSMState);
        Logger::Cleanup();
        return result;
    }
    
    // 确定是能力项名称还是设备名称
    // 先假设是能力项，尝试转换为能力ID
    TW_UINT16 capValue = 0;
    bool isCapability = false;
    
    // 搜索常见能力项
    if (strcmp(capOrDevice, "ICAP_SUPPORTEDSIZES") == 0) {
        capValue = ICAP_SUPPORTEDSIZES;
        isCapability = true;
    } else if (strcmp(capOrDevice, "ICAP_BITDEPTH") == 0) {
        capValue = ICAP_BITDEPTH;
        isCapability = true;
    } else if (strcmp(capOrDevice, "ICAP_PIXELTYPE") == 0) {
        capValue = ICAP_PIXELTYPE;
        isCapability = true;
    } else if (strcmp(capOrDevice, "ICAP_UNITS") == 0) {
        capValue = ICAP_UNITS;
        isCapability = true;
    } else if (strcmp(capOrDevice, "ICAP_XFERMECH") == 0) {
        capValue = ICAP_XFERMECH;
        isCapability = true;
    } else if (strcmp(capOrDevice, "ICAP_COMPRESSION") == 0) {
        capValue = ICAP_COMPRESSION;
        isCapability = true;
    } else if (strcmp(capOrDevice, "ICAP_IMAGEFILEFORMAT") == 0) {
        capValue = ICAP_IMAGEFILEFORMAT;
        isCapability = true;
    } else if (strcmp(capOrDevice, "ICAP_XRESOLUTION") == 0) {
        capValue = ICAP_XRESOLUTION;
        isCapability = true;
    } else if (strcmp(capOrDevice, "ICAP_YRESOLUTION") == 0) {
        capValue = ICAP_YRESOLUTION;
        isCapability = true;
    } else if (strcmp(capOrDevice, "CAP_FEEDERENABLED") == 0) {
        capValue = CAP_FEEDERENABLED;
        isCapability = true;
    } else if (strcmp(capOrDevice, "CAP_DUPLEXENABLED") == 0) {
        capValue = CAP_DUPLEXENABLED;
        isCapability = true;
    } else if (strcmp(capOrDevice, "CAP_AUTOFEED") == 0) {
        capValue = CAP_AUTOFEED;
        isCapability = true;
    } else if (strncmp(capOrDevice, "0x", 2) == 0) {
        // 尝试解析十六进制值
        capValue = (TW_UINT16)strtol(capOrDevice, NULL, 16);
        isCapability = true;
    } else {
        // 尝试解析为十进制数值
        char* endptr;
        capValue = (TW_UINT16)strtol(capOrDevice, &endptr, 10);
        if (*endptr == '\0') {
            isCapability = true;
        }
    }
    
    if (isCapability) {
        // 是能力项，获取其支持的值
        TW_CAPABILITY cap;
        memset(&cap, 0, sizeof(TW_CAPABILITY));
        cap.Cap = capValue;
        cap.ConType = TWON_DONTCARE16;
        
        // 获取能力项的当前值
        TW_UINT16 rc = gpTwainApplicationCMD->DSM_Entry(
            DG_CONTROL, DAT_CAPABILITY, MSG_GET, (TW_MEMREF)&cap);
        
        if (rc != TWRC_SUCCESS) {
            Logger::Log("@ERROR Failed to get capability: TWAIN error %d", rc);
            Logger::Cleanup();
            return result;
        }
        
        // 根据返回的容器类型处理
        strcpy(result, "[");
        int offset = 1;
        
        if (cap.ConType == TWON_ONEVALUE) {
            pTW_ONEVALUE pVal = (pTW_ONEVALUE)_DSM_LockMemory(cap.hContainer);
            if (pVal) {
                const char* typeName = convertItemTypeToString(pVal->ItemType);
                offset += sprintf(result + offset, "{\"type\":\"%s\",\"value\":", typeName);
                
                // 根据类型格式化值
                switch (pVal->ItemType) {
                    case TWTY_INT8:
                    case TWTY_INT16:
                    case TWTY_INT32:
                    case TWTY_UINT8:
                    case TWTY_UINT16:
                    case TWTY_UINT32:
                    case TWTY_BOOL:
                        offset += sprintf(result + offset, "%d", (int)pVal->Item);
                        break;
                    case TWTY_FIX32:
                        {
                            TW_FIX32 fix32;
                            memcpy(&fix32, &pVal->Item, sizeof(TW_FIX32));
                            float fVal = fix32.Whole + fix32.Frac / 65536.0f;
                            offset += sprintf(result + offset, "%.2f", fVal);
                        }
                        break;
                    default:
                        offset += sprintf(result + offset, "\"%s\"", "unsupported type");
                        break;
                }
                
                offset += sprintf(result + offset, ",\"label\":\"%s\"}", getCapabilityValueLabel(capValue, (int)pVal->Item));
                _DSM_UnlockMemory(cap.hContainer);
            }
        }
        else if (cap.ConType == TWON_ENUMERATION) {
            pTW_ENUMERATION pEnum = (pTW_ENUMERATION)_DSM_LockMemory(cap.hContainer);
            if (pEnum) {
                const char* typeName = convertItemTypeToString(pEnum->ItemType);
                
                // 处理枚举中的每个值
                for (TW_UINT32 i = 0; i < pEnum->NumItems; i++) {
                    if (i > 0) {
                        offset += sprintf(result + offset, ",");
                    }
                    
                    offset += sprintf(result + offset, "{\"type\":\"%s\",\"value\":", typeName);
                    
                    // 获取值并根据类型格式化
                    TW_UINT32 itemValue = 0;
                    switch (pEnum->ItemType) {
                        case TWTY_INT8:
                            itemValue = *(((TW_INT8*)(&pEnum->ItemList)) + i);
                            break;
                        case TWTY_INT16:
                            itemValue = *(((TW_INT16*)(&pEnum->ItemList)) + i);
                            break;
                        case TWTY_INT32:
                            itemValue = *(((TW_INT32*)(&pEnum->ItemList)) + i);
                            break;
                        case TWTY_UINT8:
                            itemValue = *(((TW_UINT8*)(&pEnum->ItemList)) + i);
                            break;
                        case TWTY_UINT16:
                            itemValue = *(((TW_UINT16*)(&pEnum->ItemList)) + i);
                            break;
                        case TWTY_UINT32:
                            itemValue = *(((TW_UINT32*)(&pEnum->ItemList)) + i);
                            break;
                        case TWTY_BOOL:
                            itemValue = *(((TW_BOOL*)(&pEnum->ItemList)) + i);
                            break;
                        case TWTY_FIX32:
                            {
                                TW_FIX32 fix32 = *(((TW_FIX32*)(&pEnum->ItemList)) + i);
                                float fVal = fix32.Whole + fix32.Frac / 65536.0f;
                                offset += sprintf(result + offset, "%.2f", fVal);
                                offset += sprintf(result + offset, ",\"label\":\"%s\"}", getCapabilityValueLabel(capValue, i));
                                continue;
                            }
                            break;
                        default:
                            offset += sprintf(result + offset, "\"%s\"", "unsupported type");
                            offset += sprintf(result + offset, ",\"label\":\"%s\"}", getCapabilityValueLabel(capValue, i));
                            continue;
                    }
                    
                    offset += sprintf(result + offset, "%d", (int)itemValue);
                    offset += sprintf(result + offset, ",\"label\":\"%s\"}", getCapabilityValueLabel(capValue, (int)itemValue));
                }
                
                _DSM_UnlockMemory(cap.hContainer);
            }
        }
        else if (cap.ConType == TWON_RANGE) {
            pTW_RANGE pRange = (pTW_RANGE)_DSM_LockMemory(cap.hContainer);
            if (pRange) {
                const char* typeName = convertItemTypeToString(pRange->ItemType);
                
                // 添加最小值
                offset += sprintf(result + offset, "{\"type\":\"%s\",\"value\":", typeName);
                if (pRange->ItemType == TWTY_FIX32) {
                    TW_FIX32 fix32;
                    memcpy(&fix32, &pRange->MinValue, sizeof(TW_FIX32));
                    float fVal = fix32.Whole + fix32.Frac / 65536.0f;
                    offset += sprintf(result + offset, "%.2f", fVal);
                } else {
                    offset += sprintf(result + offset, "%d", (int)pRange->MinValue);
                }
                offset += sprintf(result + offset, ",\"label\":\"Minimum\"},");
                
                // 添加最大值
                offset += sprintf(result + offset, "{\"type\":\"%s\",\"value\":", typeName);
                if (pRange->ItemType == TWTY_FIX32) {
                    TW_FIX32 fix32;
                    memcpy(&fix32, &pRange->MaxValue, sizeof(TW_FIX32));
                    float fVal = fix32.Whole + fix32.Frac / 65536.0f;
                    offset += sprintf(result + offset, "%.2f", fVal);
                } else {
                    offset += sprintf(result + offset, "%d", (int)pRange->MaxValue);
                }
                offset += sprintf(result + offset, ",\"label\":\"Maximum\"},");
                
                // 添加步长
                offset += sprintf(result + offset, "{\"type\":\"%s\",\"value\":", typeName);
                if (pRange->ItemType == TWTY_FIX32) {
                    TW_FIX32 fix32;
                    memcpy(&fix32, &pRange->StepSize, sizeof(TW_FIX32));
                    float fVal = fix32.Whole + fix32.Frac / 65536.0f;
                    offset += sprintf(result + offset, "%.2f", fVal);
                } else {
                    offset += sprintf(result + offset, "%d", (int)pRange->StepSize);
                }
                offset += sprintf(result + offset, ",\"label\":\"Step\"},");
                
                // 添加默认值
                offset += sprintf(result + offset, "{\"type\":\"%s\",\"value\":", typeName);
                if (pRange->ItemType == TWTY_FIX32) {
                    TW_FIX32 fix32;
                    memcpy(&fix32, &pRange->DefaultValue, sizeof(TW_FIX32));
                    float fVal = fix32.Whole + fix32.Frac / 65536.0f;
                    offset += sprintf(result + offset, "%.2f", fVal);
                } else {
                    offset += sprintf(result + offset, "%d", (int)pRange->DefaultValue);
                }
                offset += sprintf(result + offset, ",\"label\":\"Default\"}");
                
                _DSM_UnlockMemory(cap.hContainer);
            }
        }
        
        // 关闭JSON数组
        strcat(result, "]");
        
        // 释放资源
        _DSM_Free(cap.hContainer);
    }
    else {
        // 不是能力项，可能是设备名称，调用现有的获取能力列表函数
        char* allCaps = zhx_GetDevCapability_JSON(capOrDevice);
        strcpy(result, allCaps);
    }
    
    Logger::Cleanup();
    return result;
}

/**
 * 转换项目类型为字符串
 */
const char* convertItemTypeToString(TW_UINT16 itemType) {
    switch (itemType) {
        case TWTY_INT8:     return "INT8";
        case TWTY_INT16:    return "INT16";
        case TWTY_INT32:    return "INT32";
        case TWTY_UINT8:    return "UINT8";
        case TWTY_UINT16:   return "UINT16";
        case TWTY_UINT32:   return "UINT32";
        case TWTY_BOOL:     return "BOOL";
        case TWTY_FIX32:    return "FIX32";
        case TWTY_FRAME:    return "FRAME";
        case TWTY_STR32:    return "STR32";
        case TWTY_STR64:    return "STR64";
        case TWTY_STR128:   return "STR128";
        case TWTY_STR255:   return "STR255";
        default:            return "UNKNOWN";
    }
}

/**
 * 获取能力值的标签
 */
const char* getCapabilityValueLabel(TW_UINT16 capValue, int itemValue) {
    // 根据能力类型返回对应值的标签
    switch (capValue) {
        case ICAP_SUPPORTEDSIZES:
            switch (itemValue) {
                case TWSS_NONE:        return "None";
                case TWSS_A4:          return "A4";
                case TWSS_JISB5:       return "JIS B5";
                case 3:                return "US Letter";
                case TWSS_USLEGAL:     return "US Legal";
                case TWSS_A5:          return "A5";
                case TWSS_ISOB4:       return "ISO B4";
                case TWSS_ISOB6:       return "ISO B6";
                case 9:                return "US Executive";
                case TWSS_A3:          return "A3";
                case TWSS_ISOB3:       return "ISO B3";
                case TWSS_A6:          return "A6";
                case TWSS_C4:          return "C4";
                case TWSS_C5:          return "C5";
                case TWSS_C6:          return "C6";
                case TWSS_4A0:         return "4A0";
                case TWSS_2A0:         return "2A0";
                case TWSS_A0:          return "A0";
                case TWSS_A1:          return "A1";
                case TWSS_A2:          return "A2";
                case TWSS_A7:          return "A7";
                case TWSS_A8:          return "A8";
                case TWSS_A9:          return "A9";
                case TWSS_A10:         return "A10";
                case TWSS_ISOB0:       return "ISO B0";
                case TWSS_ISOB1:       return "ISO B1";
                case TWSS_ISOB2:       return "ISO B2";
                case TWSS_ISOB5:       return "ISO B5";
                case TWSS_ISOB7:       return "ISO B7";
                case TWSS_ISOB8:       return "ISO B8";
                case TWSS_ISOB9:       return "ISO B9";
                case TWSS_ISOB10:      return "ISO B10";
                case TWSS_JISB0:       return "JIS B0";
                case TWSS_JISB1:       return "JIS B1";
                case TWSS_JISB2:       return "JIS B2";
                case TWSS_JISB3:       return "JIS B3";
                case TWSS_JISB4:       return "JIS B4";
                case TWSS_JISB6:       return "JIS B6";
                case TWSS_JISB7:       return "JIS B7";
                case TWSS_JISB8:       return "JIS B8";
                case TWSS_JISB9:       return "JIS B9";
                case TWSS_JISB10:      return "JIS B10";
                default:               return "Unknown Paper Size";
            }
            
        case ICAP_PIXELTYPE:
            switch (itemValue) {
                case TWPT_BW:          return "Black & White";
                case TWPT_GRAY:        return "Grayscale";
                case TWPT_RGB:         return "RGB";
                case TWPT_PALETTE:     return "Palette";
                case TWPT_CMY:         return "CMY";
                case TWPT_CMYK:        return "CMYK";
                case TWPT_YUV:         return "YUV";
                case TWPT_YUVK:        return "YUVK";
                case TWPT_CIEXYZ:      return "CIEXYZ";
                default:               return "Unknown Pixel Type";
            }
            
        case ICAP_UNITS:
            switch (itemValue) {
                case TWUN_INCHES:      return "Inches";
                case TWUN_CENTIMETERS: return "Centimeters";
                case TWUN_PICAS:       return "Picas";
                case TWUN_POINTS:      return "Points";
                case TWUN_TWIPS:       return "Twips";
                case TWUN_PIXELS:      return "Pixels";
                case TWUN_MILLIMETERS: return "Millimeters";
                default:               return "Unknown Unit";
            }
            
        case ICAP_XFERMECH:
            switch (itemValue) {
                case TWSX_NATIVE:      return "Native";
                case TWSX_FILE:        return "File";
                case TWSX_MEMORY:      return "Memory";
                case TWSX_MEMFILE:     return "Memory File";
                default:               return "Unknown Transfer Mechanism";
            }
            
        case ICAP_IMAGEFILEFORMAT:
            switch (itemValue) {
                case TWFF_TIFF:        return "TIFF";
                case TWFF_PICT:        return "PICT";
                case TWFF_BMP:         return "BMP";
                case TWFF_XBM:         return "XBM";
                case TWFF_JFIF:        return "JPEG";
                case TWFF_FPX:         return "FlashPix";
                case TWFF_TIFFMULTI:   return "Multi-page TIFF";
                case TWFF_PNG:         return "PNG";
                case TWFF_SPIFF:       return "SPIFF";
                case TWFF_EXIF:        return "EXIF";
                case TWFF_PDF:         return "PDF";
                case TWFF_JP2:         return "JPEG 2000";
                case TWFF_JPX:         return "JPEG 2000 Extended";
                case TWFF_DEJAVU:      return "DEJAVU";
                case TWFF_PDFA:        return "PDF/A";
                case TWFF_PDFA2:       return "PDF/A-2";
                default:               return "Unknown File Format";
            }
            
        default:
            // 对于其他能力值，直接返回数值的字符串表示
            static char numLabel[20];
            sprintf(numLabel, "Value %d", itemValue);
            return numLabel;
    }
}




/**
 * Set a TWAIN capability based on string parameters
 * 
 * @param nCap String representation of the capability name (e.g. "ICAP_BITDEPTH")
 * @param value String value to set (e.g. "24")
 * @return 0 on success, error code on failure
 */
int zhx_SetCapability_STR(char *nCap, char *value) {
    Logger::Init();
    Logger::Log("@INFO Setting capability: %s to %s", nCap, value);

    // Check if TWAIN environment is initialized
    if (!gpTwainApplicationCMD) {
        Logger::Log("@ERROR TWAIN environment not initialized");
        Logger::Cleanup();
        return -1;
    }
    
    // Check if scanner is open
    if (gpTwainApplicationCMD->m_DSMState < 4) {
        Logger::Log("@ERROR Not connected to scanning device, current state: %d", gpTwainApplicationCMD->m_DSMState);
        Logger::Cleanup();
        return -2;
    }
    
    // Convert capability string to numeric value
    TW_UINT16 capValue = 0;
    bool capFound = false;
    
    // Search for capability by name
    // Common capabilities
    if (strcmp(nCap, "ICAP_BITDEPTH") == 0) {
        capValue = ICAP_BITDEPTH;
        capFound = true;
    } else if (strcmp(nCap, "ICAP_PIXELTYPE") == 0) {
        capValue = ICAP_PIXELTYPE;
        capFound = true;
    } else if (strcmp(nCap, "ICAP_UNITS") == 0) {
        capValue = ICAP_UNITS;
        capFound = true;
    } else if (strcmp(nCap, "ICAP_XFERMECH") == 0) {
        capValue = ICAP_XFERMECH;
        capFound = true;
    } else if (strcmp(nCap, "ICAP_COMPRESSION") == 0) {
        capValue = ICAP_COMPRESSION;
        capFound = true;
    } else if (strcmp(nCap, "ICAP_IMAGEFILEFORMAT") == 0) {
        capValue = ICAP_IMAGEFILEFORMAT;
        capFound = true;
    } else if (strcmp(nCap, "ICAP_XRESOLUTION") == 0 || strcmp(nCap, "ICAP_YRESOLUTION") == 0) {
        capValue = (strcmp(nCap, "ICAP_XRESOLUTION") == 0) ? ICAP_XRESOLUTION : ICAP_YRESOLUTION;
        capFound = true;
    } else if (strcmp(nCap, "CAP_FEEDERENABLED") == 0) {
        capValue = CAP_FEEDERENABLED;
        capFound = true;
    } else if (strcmp(nCap, "CAP_DUPLEXENABLED") == 0) {
        capValue = CAP_DUPLEXENABLED;
        capFound = true;
    } else if (strcmp(nCap, "CAP_AUTOFEED") == 0) {
        capValue = CAP_AUTOFEED;
        capFound = true;
    } else if (strcmp(nCap, "ICAP_SUPPORTEDSIZES") == 0) {
        capValue = ICAP_SUPPORTEDSIZES;
        capFound = true;
    } else if (strcmp(nCap, "ICAP_ORIENTATION") == 0) {
        capValue = ICAP_ORIENTATION;
        capFound = true;
    } else if (strcmp(nCap, "ICAP_CONTRAST") == 0) {
        capValue = ICAP_CONTRAST;
        capFound = true;
    } else if (strcmp(nCap, "ICAP_BRIGHTNESS") == 0) {
        capValue = ICAP_BRIGHTNESS;
        capFound = true;
    } else if (strcmp(nCap, "ICAP_GAMMA") == 0) {
        capValue = ICAP_GAMMA;
        capFound = true;
    } else if (strcmp(nCap, "ICAP_THRESHOLD") == 0) {
        capValue = ICAP_THRESHOLD;
        capFound = true;
    }

    // If capability wasn't found by name
    if (!capFound) {
        // Try to parse as hexadecimal
        if (strncmp(nCap, "0x", 2) == 0) {
            capValue = (TW_UINT16)strtol(nCap, NULL, 16);
            capFound = true;
        } 
        // Try to parse as decimal
        else {
            char* endptr;
            capValue = (TW_UINT16)strtol(nCap, &endptr, 10);
            if (*endptr == '\0') {
                capFound = true;
            }
        }
    }
    
    if (!capFound) {
        Logger::Log("@ERROR Unknown capability: %s", nCap);
        Logger::Cleanup();
        return -3;
    }
    
    // Prepare capability structure
    TW_CAPABILITY cap;
    memset(&cap, 0, sizeof(TW_CAPABILITY));
    cap.Cap = capValue;
    
    // First get the current value to determine type
    cap.ConType = TWON_DONTCARE16;
    TW_UINT16 rc = gpTwainApplicationCMD->DSM_Entry(
        DG_CONTROL, DAT_CAPABILITY, MSG_GET, (TW_MEMREF)&cap);
    
    if (rc != TWRC_SUCCESS) {
        Logger::Log("@ERROR Failed to get capability %s: TWAIN error %d", nCap, rc);
        Logger::Cleanup();
        return -4;
    }
    
    // Determine container type and item type
    TW_UINT16 itemType = TWTY_UINT16; // Default
    
    if (cap.ConType == TWON_ONEVALUE) {
        pTW_ONEVALUE pVal = (pTW_ONEVALUE)_DSM_LockMemory(cap.hContainer);
        if (pVal) {
            itemType = pVal->ItemType;
            _DSM_UnlockMemory(cap.hContainer);
        }
    } else if (cap.ConType == TWON_ENUMERATION) {
        pTW_ENUMERATION pEnum = (pTW_ENUMERATION)_DSM_LockMemory(cap.hContainer);
        if (pEnum) {
            itemType = pEnum->ItemType;
            _DSM_UnlockMemory(cap.hContainer);
        }
    } else if (cap.ConType == TWON_RANGE) {
        pTW_RANGE pRange = (pTW_RANGE)_DSM_LockMemory(cap.hContainer);
        if (pRange) {
            itemType = pRange->ItemType;
            _DSM_UnlockMemory(cap.hContainer);
        }
    }
    
    _DSM_Free(cap.hContainer);
    
    // Now create a new container to set the value
    pTW_ONEVALUE pOneValue = NULL;
    
    // Allocate memory for the container
    cap.hContainer = _DSM_Alloc(sizeof(TW_ONEVALUE));
    if (!cap.hContainer) {
        Logger::Log("@ERROR Memory allocation failed");
        Logger::Cleanup();
        return -5;
    }
    
    pOneValue = (pTW_ONEVALUE)_DSM_LockMemory(cap.hContainer);
    if (!pOneValue) {
        _DSM_Free(cap.hContainer);
        Logger::Log("@ERROR Failed to lock memory");
        Logger::Cleanup();
        return -6;
    }
    
    cap.ConType = TWON_ONEVALUE;
    pOneValue->ItemType = itemType;
    
    // Set the value based on the item type
    switch (itemType) {
        case TWTY_INT8:
        case TWTY_UINT8:
            pOneValue->Item = (TW_UINT32)atoi(value);
            Logger::Log("@INFO Setting UINT8 value: %d", (TW_UINT8)pOneValue->Item);
            break;
            
        case TWTY_INT16:
        case TWTY_UINT16:
            pOneValue->Item = (TW_UINT32)atoi(value);
            Logger::Log("@INFO Setting UINT16 value: %d", (TW_UINT16)pOneValue->Item);
            break;
            
        case TWTY_INT32:
        case TWTY_UINT32:
            pOneValue->Item = (TW_UINT32)atol(value);
            Logger::Log("@INFO Setting UINT32 value: %d", pOneValue->Item);
            break;
            
        case TWTY_BOOL:
            if (strcmp(value, "true") == 0 || strcmp(value, "1") == 0) {
                pOneValue->Item = 1;
            } else {
                pOneValue->Item = 0;
            }
            Logger::Log("@INFO Setting BOOL value: %s", pOneValue->Item ? "TRUE" : "FALSE");
            break;
            
        case TWTY_FIX32:
            {
                // Convert value string to float
                float fValue = (float)atof(value);
                
                // Convert float to FIX32
                TW_FIX32 fix32;
                fix32.Whole = (TW_INT16)fValue;
                fix32.Frac = (TW_UINT16)((fValue - fix32.Whole) * 65536.0f);
                
                // Copy FIX32 bytes to Item
                memcpy(&pOneValue->Item, &fix32, sizeof(TW_FIX32));
                Logger::Log("@INFO Setting FIX32 value: %f", fValue);
            }
            break;
            
        case TWTY_FRAME:
            Logger::Log("@ERROR FRAME type not supported for string-based setting");
            _DSM_UnlockMemory(cap.hContainer);
            _DSM_Free(cap.hContainer);
            Logger::Cleanup();
            return -7;
            
        case TWTY_STR32:
        case TWTY_STR64:
        case TWTY_STR128:
        case TWTY_STR255:
            Logger::Log("@ERROR String types not supported for string-based setting");
            _DSM_UnlockMemory(cap.hContainer);
            _DSM_Free(cap.hContainer);
            Logger::Cleanup();
            return -8;
            
        default:
            Logger::Log("@ERROR Unsupported item type: %d", itemType);
            _DSM_UnlockMemory(cap.hContainer);
            _DSM_Free(cap.hContainer);
            Logger::Cleanup();
            return -9;
    }
    
    _DSM_UnlockMemory(cap.hContainer);
    
    // Set the capability
    rc = gpTwainApplicationCMD->DSM_Entry(
        DG_CONTROL, DAT_CAPABILITY, MSG_SET, (TW_MEMREF)&cap);
    
    _DSM_Free(cap.hContainer);
    
    if (rc != TWRC_SUCCESS) {
        Logger::Log("@ERROR Failed to set capability %s to %s: TWAIN error %d", nCap, value, rc);
        Logger::Cleanup();
        return -10;
    }
    
    Logger::Log("@INFO Successfully set capability %s to %s", nCap, value);
    Logger::Cleanup();
    return 0;
}