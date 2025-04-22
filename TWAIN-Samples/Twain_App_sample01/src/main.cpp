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
#include <windows.h>

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
  // 设置回调函数
	pTW_CALLBACK callbackFunc=(pTW_CALLBACK)ImageCallback;
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
  if(!gpTwainApplicationCMD->enableDS(GetDesktopWindow(), FALSE,callbackFunc))
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
        // Get scanner list (semicolon-separated string)
        const char* scannerList = gpTwainApplicationCMD->getAvailableDataSources();
        Logger::Log("@INFO Retrieved scanner list: %s", scannerList);

        // Find scanner name and get index
        int deviceIndex = -1;
        int currentIndex = 0;

        // Create a copy of the scanner list for parsing
        char* scannerListCopy = _strdup(scannerList);
        if (!scannerListCopy) {
            Logger::Log("@ERROR Memory allocation failed");
            printf("[DLL ERROR] Memory allocation failed\n");
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
            Logger::Log("@ERROR Could not find scanner named '%s'", device);
            printf("[DLL ERROR] Could not find scanner named '%s'\n", device);
            return 0;
        }

        // Convert zero-based index to one-based device number
        int deviceNumber = deviceIndex + 1;
        Logger::Log("@INFO Found device at index %d, using device number %d", deviceIndex, deviceNumber);

        // Load the found scanner using device number (not index)
        gpTwainApplicationCMD->loadDS(deviceNumber);

        // Verify scanner loaded successfully (should be in state 4)
        if (gpTwainApplicationCMD->m_DSMState != 4) {
            Logger::Log("@ERROR Failed to load scanner, current state: %d", gpTwainApplicationCMD->m_DSMState);
            printf("[DLL ERROR] Failed to load scanner\n");
            return 0;
        }

        Logger::Log("@INFO Successfully loaded scanner '%s', index: %d", device, deviceIndex);
        printf("[DLL INFO] Successfully loaded scanner '%s', index: %d\n", device, deviceIndex);
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


// 修正后的回调函数实现
TW_UINT16 CALLBACK ImageCallback(pTW_IDENTITY pOrigin, 
                                 pTW_IDENTITY pDest, 
                                 TW_UINT32 DG,
                                 TW_UINT16 DAT,
                                 TW_UINT16 MSG,
                                 TW_MEMREF pData)
{
    UNUSEDARG(pDest);  // 未使用的参数标记
    UNUSEDARG(DG);     // 未使用的参数标记
    UNUSEDARG(DAT);    // 未使用的参数标记
    
    // 确保来源是我们的数据源
    if (0 == pOrigin || pOrigin->Id != gpTwainApplicationCMD->getDataSource()->Id)
    {
        return TWRC_FAILURE;
    }
    
    // 处理不同的消息类型
    switch (MSG)
    {
        case MSG_XFERREADY:
            // 图像传输准备就绪
            PrintCMDMessage("ImageCallback: Transfer is ready\n");
            
            // 如果pData包含图像数据，可以在这里处理
            if (pData != NULL)
            {
                // 转换为正确的指针类型
                LPBITMAPINFOHEADER bmpInfoHeader = reinterpret_cast<LPBITMAPINFOHEADER>(pData);
                
                // 获取图像数据
                BYTE* imageData = reinterpret_cast<BYTE*>(bmpInfoHeader + 1);
                
                // 输出图像宽度和高度信息
                PrintCMDMessage("Image Width: %d\n", bmpInfoHeader->biWidth);
                PrintCMDMessage("Image Height: %d\n", bmpInfoHeader->biHeight);
            }
            
            // 设置消息状态
            gpTwainApplicationCMD->m_DSMessage = MSG;
            break;
            
        case MSG_CLOSEDSREQ:
        case MSG_CLOSEDSOK:
        case MSG_NULL:
            // 设置对应消息状态
            gpTwainApplicationCMD->m_DSMessage = MSG;
            break;
            
        default:
            // 未知消息
            PrintCMDMessage("ImageCallback: Unknown message received: %d\n", MSG);
            return TWRC_FAILURE;
    }
    
    // Linux下需要发送信号量
#ifdef TWNDS_OS_LINUX
    {
        sem_post(&(gpTwainApplicationCMD->m_TwainEvent)); // Event semaphore Handle
    }
#endif
    
    return TWRC_SUCCESS;
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


void checkSupportedFormats() {
    Logger::Log("@INFO check supported formats of scanner...");
    
    TW_CAPABILITY cap;
    cap.Cap = ICAP_IMAGEFILEFORMAT;
    cap.ConType = TWON_DONTCARE16;
    cap.hContainer = NULL;

    TW_UINT16 rc = gpTwainApplicationCMD->DSM_Entry(
        DG_CONTROL, DAT_CAPABILITY, MSG_GET, (TW_MEMREF)&cap);

    if (rc == TWRC_SUCCESS) {
        pTW_ENUMERATION pValues = (pTW_ENUMERATION)_DSM_LockMemory(cap.hContainer);
        if (pValues) {
            Logger::Log("@INFO scanner supports %d file formats:", pValues->NumItems);
            
            for (TW_UINT32 i = 0; i < pValues->NumItems; i++) {
                TW_UINT16 format = ((pTW_UINT16)(&pValues->ItemList))[i];
                const char* ext = convertICAP_IMAGEFILEFORMAT_toExt(format);
                Logger::Log("@INFO    format %d: %s", format, ext);
            }
            
            // 检查当前设置的格式
            TW_UINT16 currentFormat = ((pTW_UINT16)(&pValues->ItemList))[pValues->CurrentIndex];
            Logger::Log("@INFO current default format: %d (%s)", 
                       currentFormat, 
                       convertICAP_IMAGEFILEFORMAT_toExt(currentFormat));
            
            // 特别检查是否支持PNG
            bool supportsPNG = false;
            for (TW_UINT32 i = 0; i < pValues->NumItems; i++) {
                if (((pTW_UINT16)(&pValues->ItemList))[i] == TWFF_PNG) {
                    supportsPNG = true;
                    break;
                }
            }
            
            if (supportsPNG) {
                Logger::Log("@INFO scanner supports PNG format");
            } else {
                Logger::Log("@WARNING scanner does not support PNG format!");
            }
            
            _DSM_UnlockMemory(cap.hContainer);
        }
        _DSM_Free(cap.hContainer);
    } else {
        Logger::Log("@ERROR failed to query scanner supported formats, error code: %d", rc);
    }
}