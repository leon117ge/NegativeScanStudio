// Stub for the box compile check only (see ../README.md). Not the Apple header.
// Declarations follow the macOS 13 SDK (ICDevice.h, ICCameraDevice.h, ICDeviceBrowser.h).
#pragma once
#import <Foundation/Foundation.h>
NS_ASSUME_NONNULL_BEGIN
typedef struct CGImage *CGImageRef;
typedef NS_OPTIONS(NSUInteger, ICDeviceTypeMask) { ICDeviceTypeMaskCamera = 0x00000001, ICDeviceTypeMaskScanner = 0x00000002 };
typedef NS_OPTIONS(NSUInteger, ICDeviceLocationTypeMask) {
	ICDeviceLocationTypeMaskLocal = 0x00000100,
	ICDeviceLocationTypeMaskShared = 0x00000200,
	ICDeviceLocationTypeMaskBonjour = 0x00000400,
	ICDeviceLocationTypeMaskBluetooth = 0x00000800,
	ICDeviceLocationTypeMaskRemote = 0x0000FE00
};
typedef NSString *ICAuthorizationStatus;
extern ICAuthorizationStatus const ICAuthorizationStatusNotDetermined;
extern ICAuthorizationStatus const ICAuthorizationStatusRestricted;
extern ICAuthorizationStatus const ICAuthorizationStatusDenied;
extern ICAuthorizationStatus const ICAuthorizationStatusAuthorized;
extern NSString *const ICCameraDeviceCanAcceptPTPCommands;
@class ICDevice;
@class ICCameraDevice;
@class ICCameraItem;
@class ICDeviceBrowser;
@protocol ICDeviceDelegate <NSObject>
@required
- (void)didRemoveDevice:(ICDevice *)device;
@optional
- (void)device:(ICDevice *)device didOpenSessionWithError:(nullable NSError *)error;
- (void)deviceDidBecomeReady:(ICDevice *)device;
- (void)device:(ICDevice *)device didCloseSessionWithError:(nullable NSError *)error;
@end
@interface ICDevice : NSObject
@property(nullable, weak) id<ICDeviceDelegate> delegate;
@property(readonly, copy, nullable) NSString *name;
@property(readonly, copy, nullable) NSString *UUIDString;
@property(readonly, copy, nullable) NSString *serialNumberString;
@property(readonly) int usbVendorID;
@property(readonly) int usbProductID;
@property(readonly) NSArray<NSString *> *capabilities;
- (void)requestOpenSessionWithOptions:(nullable NSDictionary<NSString *, id> *)options completion:(void (^)(NSError *_Nullable error))completion;
- (void)requestCloseSessionWithOptions:(nullable NSDictionary<NSString *, id> *)options completion:(void (^)(NSError *_Nullable error))completion;
@end
@interface ICCameraItem : NSObject
@end
@protocol ICCameraDeviceDelegate <ICDeviceDelegate>
@required
- (void)cameraDevice:(ICCameraDevice *)camera didAddItems:(NSArray<ICCameraItem *> *)items;
- (void)cameraDevice:(ICCameraDevice *)camera didRemoveItems:(NSArray<ICCameraItem *> *)items;
- (void)cameraDevice:(ICCameraDevice *)camera didReceiveThumbnail:(nullable CGImageRef)thumbnail forItem:(ICCameraItem *)item error:(nullable NSError *)error;
- (void)cameraDevice:(ICCameraDevice *)camera didReceiveMetadata:(nullable NSDictionary *)metadata forItem:(ICCameraItem *)item error:(nullable NSError *)error;
- (void)cameraDevice:(ICCameraDevice *)camera didRenameItems:(NSArray<ICCameraItem *> *)items;
- (void)cameraDeviceDidChangeCapability:(ICCameraDevice *)camera;
- (void)cameraDevice:(ICCameraDevice *)camera didReceivePTPEvent:(NSData *)eventData;
- (void)deviceDidBecomeReadyWithCompleteContentCatalog:(ICCameraDevice *)device;
- (void)cameraDeviceDidRemoveAccessRestriction:(ICDevice *)device;
- (void)cameraDeviceDidEnableAccessRestriction:(ICDevice *)device;
@end
@interface ICCameraDevice : ICDevice
- (void)requestSendPTPCommand:(NSData *)ptpCommand
                      outData:(nullable NSData *)ptpData
                   completion:(void (^)(NSData *responseData, NSData *ptpResponseData, NSError *_Nullable error))completion;
@end
@protocol ICDeviceBrowserDelegate <NSObject>
@required
- (void)deviceBrowser:(ICDeviceBrowser *)browser didAddDevice:(ICDevice *)device moreComing:(BOOL)moreComing;
- (void)deviceBrowser:(ICDeviceBrowser *)browser didRemoveDevice:(ICDevice *)device moreGoing:(BOOL)moreGoing;
@optional
- (void)deviceBrowserDidEnumerateLocalDevices:(ICDeviceBrowser *)browser;
@end
@interface ICDeviceBrowser : NSObject
@property(nullable, weak) id<ICDeviceBrowserDelegate> delegate;
@property ICDeviceTypeMask browsedDeviceTypeMask;
@property(readonly, nullable) NSArray<ICDevice *> *devices;
- (void)start;
- (void)stop;
- (ICAuthorizationStatus)controlAuthorizationStatus;
- (ICAuthorizationStatus)contentsAuthorizationStatus;
- (void)requestControlAuthorizationWithCompletion:(void (^)(ICAuthorizationStatus status))completion;
- (void)requestContentsAuthorizationWithCompletion:(void (^)(ICAuthorizationStatus status))completion;
@end
NS_ASSUME_NONNULL_END
