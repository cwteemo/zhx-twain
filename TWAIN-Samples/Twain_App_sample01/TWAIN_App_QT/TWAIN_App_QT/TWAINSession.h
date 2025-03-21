/***************************************************************************
* Copyright � 2010 TWAIN Working Group:  
*   Adobe Systems Incorporated, AnyDoc Software Inc., Eastman Kodak Company, 
*   Fujitsu Computer Products of America, JFL Peripheral Solutions Inc., 
*   Avision Inc., and Atalasoft, Inc.
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
 * @file TWAINSession.h
 * TWAIN Application.
 * A TWAIN Application communicates with the DSM to acquire images. 
 * The goal of the application is to acquire data from a Source.  
 * However, applications cannot contact the Source directly.  All requests for
 * data, capability information, error information, etc. must be handled 
 * Through the Source Manager.
 * @author JFL Peripheral Solutions Inc.
 * @date August 2010
 */

#ifndef _TWAINSession_H
#define _TWAINSession_H

#ifdef _WIN32
  #include "windows.h"
#endif //#ifdef _WIN32

#include <string>
#include <vector>
#include <queue>

#include "CommonTWAIN.h"
#include "FreeImage.h"

/**
* Application to abstract common Application TWAIN functionality
* @author JFL Peripheral Solutions Inc.
* @date August 2010
*/
class CTWAINSession
{
  public:
  /**
  * Constructor increments reference counts and manages initialization of statics
  */
  CTWAINSession();
  /**
  * Decrements reference count and manages destruction of statics
  */
  ~CTWAINSession();
  /**
  * Called to establish the identity of this application (may also be done directly by the derived class constructor)
  * @param[in] twIdentity reference to the ID to copy
  */
  void SetApplicationIdentity(const TW_IDENTITY &twIdentity);
  /**
  * Loads the DataSource Manager
  * Must be called in State 1
  * Success indicates a transition to State 2
  * @return TWRC_SUCCESS if successful, otherwise TWRC_FAILURE, GetLastStatus for details
  */
  TW_UINT16 LoadDSM();
  /**
  * Frees the DataSource Manager
  * Must be called in State 2
  * Success indicates a transition to State 1
  * @return TWRC_SUCCESS if successful, otherwise TWRC_FAILURE, GetLastStatus for details
  */
  TW_UINT16 FreeDSM();
  /**
  * General method for making any TWAIN Triplet call to the DataSource Manager.
  * This method watches for key triplets, manages the internal State tracking, manages last known Status
  * and dispatches OnError calls (when enabled).
  * @param[in] uiDG TWAIN Data Group, can be any defined DG_ value
  * @param[in] uiDAT TWAIN Data Argument type, can be any defined DAT_ value, describes pData paramater
  * @param[in] uiMSG TWAIN Message, defines the Operation task, can be any defined MSG_ value
  * @param[in|out] pData pointer to the Operation data, must be structure corresponding to uiDAT argument, no type checking
  * @param[in] pDataSource optional pointer to the destination data source, default is NULL
  * @return TWRC_SUCCESS if successful, otherwise any TWRC_ defined value depending on Triplet values
  */
  TW_UINT16 DSM_Entry(TW_UINT32 uiDG, TW_UINT16 uiDAT, TW_UINT16 uiMSG, TW_MEMREF pData, pTW_IDENTITY pDataSource=NULL);
  /**
  * Method to obtain the TWAIN State for the current TWAIN Session
  * @return the current TWAIN Session State, a value from 1-7
  */
  TW_UINT16 GetCurrentState();
  /**
  * Method to obtain the last known TWAIN Status
  * @param[in] twStatus destination for copy of last known status information
  */
  void GetLastStatus(TW_STATUS &twStatus);
  /**
  * Method to Open the TWAIN DSM
  * Must be called in State 2
  * Success indicates a transition to State 3
  * @param[in] the parent window handle (OS dependent)
  * @return TWRC_SUCCESS if successful, otherwise TWRC_FAILURE, GetLastStatus for details
  */
  TW_UINT16 OpenDSM(TW_MEMREF mrParent);
  /**
  * Method to Close the TWAIN DSM
  * Must be called in State 3
  * Success indicates a transition to State 2
  * @return TWRC_SUCCESS if successful, otherwise TWRC_FAILURE, GetLastStatus for details
  */
  TW_UINT16 CloseDSM();
  /**
  * Method fills the identity structure with the first ID containing the supplied product name
  * @param[in] pszProductName the product name to look for
  * @param[out] twIdentity the identity to fill
  * @return TWRC_SUCCESS if successful, otherwise TWRC_FAILURE, GetLastStatus for details
  */
  TW_UINT16 GetDataSourceID(const char *pszProductName, TW_IDENTITY &twIdentity);
  /**
  * Method to Open the default TWAIN DS
  * Must be called in State 3
  * Success indicates a transition to State 4
  * @return TWRC_SUCCESS if successful, otherwise TWRC_FAILURE, GetLastStatus for details
  */
  TW_UINT16 OpenDS();
  /**
  * Method to Open First TWAIN DS with ProductName
  * Must be called in State 3
  * Success indicates a transition to State 4
  * @return TWRC_SUCCESS if successful, otherwise TWRC_FAILURE, GetLastStatus for details
  */
  TW_UINT16 OpenDS(const char *pszProductName);
  /**
  * Method to Open First TWAIN DS with indicated TW_IDENTITY
  * Must be called in State 3
  * Success indicates a transition to State 4
  * @return TWRC_SUCCESS if successful, otherwise TWRC_FAILURE, GetLastStatus for details
  */
  TW_UINT16 OpenDS(TW_IDENTITY &twSource);
  /**
  * Method to Close the TWAIN DS
  * Must be called in State 3
  * Success indicates a transition to State 4
  * @return TWRC_SUCCESS if successful, otherwise TWRC_FAILURE, GetLastStatus for details
  */
  TW_UINT16 CloseDS();
  /**
  * Method to hookup the internal callback
  * Must be called in State 4
  * @return TWRC_SUCCESS if successful, otherwise TWRC_FAILURE, GetLastStatus for details
  */
  TW_UINT16 SetCallback();
  /**
  * Basic method for setting a capability
  * @param[in] twCap the description of the capability and container to set
  * @return TWRC_SUCCESS if successful, otherwise TWRC_FAILURE, GetLastStatus for details
  */
  TW_UINT16 SetCapability(TW_CAPABILITY &twCap);
  /**
  * Utility method for getting supported capability values
  * @param[in] twCapId the id of the capability to get
  * @param[out] hContainer container resulting from the successful operation
  * @param[out] twConType type of container resulting from the successful operation
  * @return TWRC_SUCCESS if successful, otherwise TWRC_FAILURE, GetLastStatus for details
  */
  TW_UINT16 GetCapabilitySupported(TW_UINT16 twCapId, TW_HANDLE &hContainer, TW_UINT16 &twConType);
  /**
  * Basic method for performing any 'get' capability operation
  * @param[in|out] twCap the description of the capability to get and to house the result
  * @param[in] twMsg must be one of MSG_GET, MSG_GETCURRENT, MSG_GETDEFAULT, MSG_RESET
  * @return TWRC_SUCCESS if successful, otherwise TWRC_FAILURE, GetLastStatus for details
  */
  TW_UINT16 GetCapability(TW_CAPABILITY &twCap, TW_UINT16 twMsg=MSG_GET);
  /**
  * Basic method to query what MSG_' operations are supported by a particular capability
  * @param[in] twCapId the id of the capability to check
  * @return a combination of TWQS_ flags or 0 if no operations are supported
  */
  TW_UINT32 QuerySupportCapability(TW_UINT16 twCapId);
  /**
  * Utility method for setting current capability values when the item type is bool
  * @param[in] twCapId the id of the capability to set
  * @param[in] bValue the boolean value to set
  * @return TWRC_SUCCESS if successful, otherwise TWRC_FAILURE, GetLastStatus for details
  */
  TW_UINT16 SetCapabilityOneValue(TW_UINT16 twCapId, const bool &bValue);
  /**
  * Utility method for setting current capability values when the item type is TW_INT16
  * @param[in] twCapId the id of the capability to set
  * @param[in] twVal the value to set
  * @return TWRC_SUCCESS if successful, otherwise TWRC_FAILURE, GetLastStatus for details
  */
  TW_UINT16 SetCapabilityOneValue(TW_UINT16 twCapId, const TW_INT16 &twVal);
  /**
  * Utility method for setting current capability values when the item type is TW_UINT16
  * @param[in] twCapId the id of the capability to set
  * @param[in] twVal the value to set
  * @return TWRC_SUCCESS if successful, otherwise TWRC_FAILURE, GetLastStatus for details
  */
  TW_UINT16 SetCapabilityOneValue(TW_UINT16 twCapId, const TW_UINT16 &twVal);
  /**
  * Utility method for setting current capability values when the item type is TW_UINT32
  * @param[in] twCapId the id of the capability to set
  * @param[in] twVal the value to set
  * @return TWRC_SUCCESS if successful, otherwise TWRC_FAILURE, GetLastStatus for details
  */
  TW_UINT16 SetCapabilityOneValue(TW_UINT16 twCapId, const TW_UINT32 &twVal);
  /**
  * Utility method for setting current capability values when the item type is TW_FIX32
  * @param[in] twCapId the id of the capability to set
  * @param[in] twVal the value to set
  * @return TWRC_SUCCESS if successful, otherwise TWRC_FAILURE, GetLastStatus for details
  */
  TW_UINT16 SetCapabilityOneValue(TW_UINT16 twCapId, const TW_FIX32 &twVal);
  /**
  * Utility method for setting current capability values when the item type is TW_FRAME
  * @param[in] twCapId the id of the capability to set
  * @param[in] twVal the value to set
  * @return TWRC_SUCCESS if successful, otherwise TWRC_FAILURE, GetLastStatus for details
  */
  TW_UINT16 SetCapabilityOneValue(TW_UINT16 twCapId, const TW_FRAME &twVal);
  /**
  * Utility method for getting current or default capability values when the item type is bool
  * @param[in] twCapId the id of the capability to get
  * @param[in] bValue the value to get
  * @param[in] twMsg must be one of MSG_GETCURRENT, MSG_GETDEFAULT, MSG_RESET
  * @return TWRC_SUCCESS if successful, otherwise TWRC_FAILURE, GetLastStatus for details
  */
  TW_UINT16 GetCapabilityOneValue(TW_UINT16 twCapId, bool &bValue, TW_UINT16 twMsg=MSG_GETCURRENT);  
  /**
  * Utility method for getting current or default capability values when the item type is TW_INT16
  * @param[in] twCapId the id of the capability to get
  * @param[in] twValue the value to get
  * @param[in] twMsg must be one of MSG_GETCURRENT, MSG_GETDEFAULT, MSG_RESET
  * @return TWRC_SUCCESS if successful, otherwise TWRC_FAILURE, GetLastStatus for details
  */
  TW_UINT16 GetCapabilityOneValue(TW_UINT16 twCapId, TW_INT16 &twValue, TW_UINT16 twMsg=MSG_GETCURRENT);  
  /**
  * Utility method for getting current or default capability values when the item type is TW_UINT16
  * @param[in] twCapId the id of the capability to get
  * @param[in] twValue the value to get
  * @param[in] twMsg must be one of MSG_GETCURRENT, MSG_GETDEFAULT, MSG_RESET
  * @return TWRC_SUCCESS if successful, otherwise TWRC_FAILURE, GetLastStatus for details
  */
  TW_UINT16 GetCapabilityOneValue(TW_UINT16 twCapId, TW_UINT16 &twValue, TW_UINT16 twMsg=MSG_GETCURRENT);  
  /**
  * Utility method for getting current or default capability values when the item type is TW_UINT32
  * @param[in] twCapId the id of the capability to get
  * @param[in] twValue the value to get
  * @param[in] twMsg must be one of MSG_GETCURRENT, MSG_GETDEFAULT, MSG_RESET
  * @return TWRC_SUCCESS if successful, otherwise TWRC_FAILURE, GetLastStatus for details
  */
  TW_UINT16 GetCapabilityOneValue(TW_UINT16 twCapId, TW_UINT32 &twValue, TW_UINT16 twMsg=MSG_GETCURRENT);  
  /**
  * Utility method for getting current or default capability values when the item type is TW_FIX32
  * @param[in] twCapId the id of the capability to get
  * @param[in] twValue the value to get
  * @param[in] twMsg must be one of MSG_GETCURRENT, MSG_GETDEFAULT, MSG_RESET
  * @return TWRC_SUCCESS if successful, otherwise TWRC_FAILURE, GetLastStatus for details
  */
  TW_UINT16 GetCapabilityOneValue(TW_UINT16 twCapId, TW_FIX32 &twValue, TW_UINT16 twMsg=MSG_GETCURRENT);  
  /**
  * Utility method for getting current or default capability values when the item type is TW_FRAME
  * @param[in] twCapId the id of the capability to get
  * @param[in] twValue the value to get
  * @param[in] twMsg must be one of MSG_GETCURRENT, MSG_GETDEFAULT, MSG_RESET
  * @return TWRC_SUCCESS if successful, otherwise TWRC_FAILURE, GetLastStatus for details
  */
  TW_UINT16 GetCapabilityOneValue(TW_UINT16 twCapId, TW_FRAME &twValue, TW_UINT16 twMsg=MSG_GETCURRENT);  
  /**
  * General method for setting a capability item of any type
  * @param[in] twCap structure describing the capabability to set and a pre-allocated TW_ONEVALUE container
  * @param[in] pValue the value of the type described by the twCap structure to be set
  * @return TWRC_SUCCESS if successful, otherwise TWRC_FAILURE, GetLastStatus for details
  */
  TW_UINT16 SetCapabilityAnyOneValue(TW_CAPABILITY &twCap, const void *pValue);
  /**
  * General method for getting a capability item of any type
  * @param[in] twCap structure describing the capabability to get
  * @param[out] pData pointer to the value to accept the resulting data
  * @param[in] twMsg must be one of MSG_GETCURRENT, MSG_GETDEFAULT, MSG_RESET
  * @return TWRC_SUCCESS if successful, otherwise TWRC_FAILURE, GetLastStatus for details
  */
  TW_UINT16 GetAnyCapabilityOneValue(TW_CAPABILITY twCap, void *pData, TW_UINT16 twMsg);
  /**
  * Method to Enable the DataSource with intent to transfer
  * Must be called in State 4
  * Success indicates a transition to State 5
  * @param[in] hParent handle to the parent window (OS Dependent)
  * @param[in] bShowUI indicates if the UI should be presented
  * @return TWRC_SUCCESS if successful, TWRC_FAILURE on failure, TWRC_CHECKSTATUS if bShowUI==false and UI has been displayed anyway due to DS design, GetLastStatus for details
  */
  TW_UINT16 EnableDS(TW_HANDLE hParent, bool bShowUI);
  /**
  * Method to Enable the DataSource with intent to configure settings
  * Must be called in State 4
  * Success indicates a transition to State 5
  * @param[in] hParent handle to the parent window (OS Dependent)
  * @return TWRC_SUCCESS if successful, TWRC_FAILURE on failure, GetLastStatus for details
  */
  TW_UINT16 EnableDSUIOnly(TW_HANDLE hParent);
  /**
  * Method to Disable the DataSource with intent to configure settings
  * Must be called in State 5
  * Success indicates a transition to State 4
  * @return TWRC_SUCCESS if successful, TWRC_FAILURE on failure, GetLastStatus for details
  */
  TW_UINT16 DisableDS();
  /**
  * Method that MUST be cranked in Windows by the thread event loop for proper UI keyboard handling and signaling events when callbacks are not used
  * @param twEvent a reference to a structure containing event information to be relayed to the DataSource
  * @return TWRC_DSEVENT if the event was consumed by the DataSource or TWRC_NOTDSEVENT if the event did not belong to the DataSource
  */
  TW_UINT16 ProcessEvent(const TW_EVENT &twEvent);
  /**
  * Accessor for the next item in a a Queue of DataSource requests
  * @return one of MSG_XFERREADY, MSG_CLOSEDSREQ, MSG_CLOSEDSOK, MSG_DEVICEEVENT
  */
  TW_UINT16 GetDSRequest();
  /**
  * Removes the current DataSource request from the internal Queue
  */
  void ClearDSRequest();
  /**
  * Method to manually invoke a transfer
  * Must be called in State 6
  * Examines the current value of CAP_XFERMECH and invokes the appropriate transfer methods and signals
  */
  TW_UINT16 DoTransfer();
  /**
  * Method to manually invoke a transfer
  * Must be called in State 6
  * Examines the current value of CAP_XFERMECH and invokes the appropriate transfer methods and signals
  */
  TW_UINT16 DoEndXfer();
  /**
  * Optional Implementation specific handler called when an event is signaled from the DS.
  * Do very little on this method as it may be in the context of a thread other than the TWAIN session.
  */
  virtual void OnSignalDSRequest();
  /**
  * Implementation specific handler called during any Operating before returning TWRC_FAILURE
  * @param[in] twStatus a reference to a structure containing the status of the recently failed method
  */
  virtual void OnError(const TW_STATUS &twStatus) = 0;
  /**
  * Optional Implementation specifical handler that is called right before an image transfer begins (state 6)
  * @param[in] twInfo contains information obtained by DAT_IMAGEINFO/MSG_GET call in State 6
  * @param[in] twXferMech contains the transfer mode being used
  */
  inline virtual void OnImageBegin(const TW_IMAGEINFO &twInfo, TW_UINT16 twXferMech);
  /**
  * Optional Implementation specifical handler that is called after TWRC_XFERDONE is recieved (state 7)
  * @param[in] twInfo contains information obtained by DAT_IMAGEINFO/MSG_GET call in State 6 (if UNDEFINEDIMAGESIZE==true then State 7)
  * @param[in] twXferMech contains the transfer mode being used
  */
  inline virtual void OnImageEnd(const TW_IMAGEINFO &twInfo, TW_UINT16 twXferMech);
  /**
  * Optional Implementation specifical handler that is called after MSG_ENDXFER is called (state 6 or 5)
  * @param[in] twXfer contains information obtained during DAT_PENDINGXFERS/MSG_ENDXFER operation during State 7
  */
  inline virtual void OnEndXfer(const TW_PENDINGXFERS &twXfer);
  /**
  * Implementation specific handler called during a native Transfer
  * @param[in] pData OS Dependent pointer/handle to native image data
  * @param[out] bAbort default value false, set to true if abort transfer is desired
  * @param[out] bFree default value is true, decides if pData should be freed by underlying transfer code
  */
  virtual void OnNativeTransfer(const TW_MEMREF &pData, bool &bAbort, bool &bFree) = 0;
  /**
  * Implementation specific handler called during a Memory Transfer
  * @param[in] twData structuring containing information about this memory transfer data
  * @param[in] twRC either TWRC_SUCCESS meaning the current image is not finished or TWRC_XFERDONE meaning this is the last block
  * @param[out] bAbort default value false, set to true if abort transfer is desired
  */
  virtual void OnMemoryTransfer(const TW_IMAGEINFO &twInfo, const TW_IMAGEMEMXFER &twData, const TW_UINT16 &twRC, bool &bAbort) = 0;
  /**
  * Implementation specific handler called during a File Transfer
  * @param[in] strPath complete path to the resulting image
  * @param[out] bAbort default value false, set to true if abort transfer is desired
  */
  virtual void OnFileTransfer(const TW_SETUPFILEXFER &twFileXfer, bool &bAbort) = 0;
  /**
  * Implementation specific handler called during a File in Memory Transfer
  * @param[in] twData structuring containing information about this memory transfer data
  * @param[in] twRC either TWRC_SUCCESS meaning the current image is not finished or TWRC_XFERDONE meaning this is the last block
  * @param[out] bAbort default value false, set to true if abort transfer is desired
  */
  virtual void OnFileMemTransfer(const TW_IMAGEMEMXFER &twData, const TW_UINT16 &twRC, bool &bAbort) = 0;
  /**
  * Signal that gives the derived class an opportunity to do what is necessary to eliminate recursive calls
  */
  inline virtual void PreDSMCall();
  /**
  * Signal that gives the derived class an opportunity to un-do whatever was necessary to eliminate recursive calls
  */
  inline virtual void PostDSMCall();
  /**
  * Method used to move the session down to the specified state
  * @param[in] twDesiredState the state requested
  * @return true if successfully transitioned to the requested state
  */
  bool ReturnToState(TW_UINT16 twDesiredState);
  /**
  * Method used to transition to one state lower
  * @return TWRC_SUCCESS if successful, TWRC_FAILURE on failure, GetLastStatus will contain details about the last executed Triplet
  */
  TW_UINT16 DownOneState();

  /**
  * Method used to enable the invocation of OnError by DSM_Entry method
  * @param[in] bEnabled set to true for Enabled and false for Disabled
  */
  void EnableOnError(bool bEnabled);

  protected:
  TW_IDENTITY m_twAppIdentity; /**< current application identity */
  TW_IDENTITY m_twSourceIdentity; /**< current destination source identity */
  TW_MEMREF m_mrParent; /**< current parent window handle (OS dependent)*/
  TW_STATUS m_twLastStatus; /**< structuring containing a copy of the last known operation status */
  TW_UINT16 m_uiState; /**< current TWAIN Session state */
  TW_UINT32 m_twInstID; /**< ID assigned to the current class instance */
  CRITICAL_SECTION m_csProtectRequestQueue; /**< critical section used to control access to the DataSource request queue */
  queue<TW_UINT16> m_twDSRequestQueue; /**< Queue of DataSource requests */
  TW_PENDINGXFERS m_twPendingXfers; /**< when transferring several images, this contains the last MSG_ENDXEER result */
  
  bool m_bStartOfStack; /**< flag is set to true on each state 5-6 transition and false on each 6 to 7 transition */
  /**
  * The following are upated updated by Transfer method once per batch (once after every state 5-6 transition)
  */
  bool m_bSupportsExtImageInfo; /**< indicates data source reports support for DAT_EXTIMAGEINFO, cached for use in state 7 */
  bool m_bUsingUndefinedImageSize; /** indicates the data source is using ICAP_UNDEFINEDIMAGESIZE transfer technique, cached for use in state 7 */
  TW_UINT16 m_twXferMech; /** indicates what transfer mode is currently being used, cached for use in state 7 */

  /**
  * Optional method for changing the destination of trace messages
  * @param[in] pszFormat a printf style format string
  * @param[in] ... stacked printf style parameters
  */
  virtual void TraceMessage(const char *pszFormat, ...);
  /**
  * Method used by the static callback function to signal a request in the current instance
  * @param[in]twMsg the current DS Request
  */
  void SignalDSRequest(TW_UINT16 twMsg);
  /**
  * Method used to initialize a TW_CAPABILITY structure and allocate a container of the appropriate size
  * @param[in] twCapId the ID of the capability being initialized
  * @param[in] twConType TWON_ type of container to use
  * @param[in] twItemType TWTY_ type of items to be housed in the container
  * @param[out] twCap reference to the TW_CAPABILITY structure used to house the result
  * @param[in] twCount optional parameter that specifies the number of items, valid only for TWON_ENUMERATION and TWON_ARRAY containers
  * @return true if successful
  */
  bool FillCapability(TW_UINT16 twCapId, TW_UINT16 twConType, TW_UINT16 twItemType, TW_CAPABILITY &twCap, TW_UINT32 twCount=1);
  /**
  * Method to allocate a TWAIN Container that results in a handle
  * @param[in] twConType TWON_ type of container to use
  * @param[in] twItemType TWTY_ type of items to be housed in the container
  * @param[in] twCount optional parameter that specifies the number of items, valid only for TWON_ENUMERATION and TWON_ARRAY containers
  * @return NULL if un-successful, otherwise a valid handle to a TWAIN Container
  */
  TW_HANDLE AllocateContainer(TW_UINT16 twConType, TW_UINT16 twItemType, TW_UINT32 twCount=1);
  /**
  * Global callback for all instances of this class
  * Invokes SignalDSRequest in the appropriate class instance
  * @return TWRC_FAILURE if unable to signal the appropriate class instance
  */
  static TW_UINT16 FAR PASCAL DSMCallback(pTW_IDENTITY pOrigin, pTW_IDENTITY pDest, TW_UINT32 uiDG, TW_UINT16 uiDAT, TW_UINT16 uiMSG, TW_MEMREF pData);

  static map<TW_UINT32, CTWAINSession *> m_mapInstance; /**< contains all instances of this class mapped by unique identifiers */
  static TW_UINT32 m_uiInstCount; /**< always contains the value of the next 'unique' identifier (has practical unsigned 16 bit integer limits) */
  static int m_nRefCount; /**< current number of instances of this class */
  static CRITICAL_SECTION m_csProtectInstanceMap; /**< critical section used to control access to the class instance map */
  bool m_bInTWAIN; /**< flag used to detect and prevent DSM re-entrancy */

};

inline void CTWAINSession::PreDSMCall()
{
  //default does nothing
  return;
}

inline void CTWAINSession::PostDSMCall()
{
  //default does nothing
  return;
}

inline void CTWAINSession::OnImageBegin(const TW_IMAGEINFO &twInfo, TW_UINT16 twXferMech)
{
  //default does nothing
  return;
}

inline void CTWAINSession::OnImageEnd(const TW_IMAGEINFO &twInfo, TW_UINT16 twXferMech)
{
  //default does nothing
  return;
}

inline void CTWAINSession::OnEndXfer(const TW_PENDINGXFERS &twXfer)
{
  //default does nothing
  return;
}

/**
* Class to automatically manage the state of a boolean flag within the scope / existance of the object
*/
class CAutoFlag
{
  public:
  /**
  * Constructor sets the flag and stores a reference
  * @param[in] bFlag a reference to the flag to manage
  */
  inline CAutoFlag(bool &bFlag);
  /**
  * Destructor resets the flag
  */
  inline ~CAutoFlag();
  protected:
  bool &m_bFlag; /**< reference to the flag to manage */
};

inline CAutoFlag::CAutoFlag(bool &bFlag)
  : m_bFlag(bFlag)
{
  //set the flag and keep a reference to it
  m_bFlag = true;
  return;
}

inline CAutoFlag::~CAutoFlag()
{
  //reset the flag
  m_bFlag = false;
  return;
}

/**
* Utility class to automatically manage the state of a critical section within the scope / existance of the object
*/
class CAutoCriticalSection
{
  public:
  /**
  * Constructor enters the critical section
  * @param[in] pCriticalSection a pointer to the critical section to manage
  */
  inline CAutoCriticalSection(CRITICAL_SECTION *pCriticalSection);
  /**
  * Destructor leaves the critical section
  */
  inline ~CAutoCriticalSection();
  protected:
  CRITICAL_SECTION *m_pCriticalSection; /**< reference to the flag to manage */
};

inline CAutoCriticalSection::CAutoCriticalSection(CRITICAL_SECTION *pCriticalSection)
  : m_pCriticalSection(pCriticalSection)
{
  //Enter the critical section
  if(m_pCriticalSection)
  {
    EnterCriticalSection(m_pCriticalSection);
  }
  return;
}

inline CAutoCriticalSection::~CAutoCriticalSection()
{
  //Leave the critical section
  if(m_pCriticalSection)
  {
    LeaveCriticalSection(m_pCriticalSection);
  }
  return;
}

#endif //#ifndef _TWAINSession_H
