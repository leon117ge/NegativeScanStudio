// Stub for the box compile check only (see ../README.md). Not the Apple header.
#pragma once
#import <objc/NSObject.h>
#import <objc/NSObjCRuntime.h>
#include <dispatch/dispatch.h>
#include <stdint.h>
#define NS_ASSUME_NONNULL_BEGIN _Pragma("clang assume_nonnull begin")
#define NS_ASSUME_NONNULL_END _Pragma("clang assume_nonnull end")
#define NS_ENUM(type, name) enum name : type name; enum name : type
#define NS_OPTIONS(type, name) enum name : type name; enum name : type
#define NS_RETURNS_INNER_POINTER __attribute__((objc_returns_inner_pointer))
typedef double NSTimeInterval;
typedef NS_ENUM(NSInteger, NSComparisonResult) { NSOrderedAscending = -1L, NSOrderedSame, NSOrderedDescending };
NS_ASSUME_NONNULL_BEGIN
typedef struct {
	unsigned long state;
	id __unsafe_unretained _Nullable *_Nullable itemsPtr;
	unsigned long *_Nullable mutationsPtr;
	unsigned long extra[5];
} NSFastEnumerationState;
@protocol NSFastEnumeration
- (NSUInteger)countByEnumeratingWithState:(NSFastEnumerationState *)state objects:(id __unsafe_unretained _Nullable[_Nonnull])buffer count:(NSUInteger)len;
@end
@interface NSString : NSObject
@property(nullable, readonly) const char *UTF8String NS_RETURNS_INNER_POINTER;
+ (nullable instancetype)stringWithUTF8String:(const char *)s;
+ (instancetype)stringWithFormat:(NSString *)format, ...;
- (BOOL)isEqualToString:(NSString *)other;
- (NSComparisonResult)caseInsensitiveCompare:(NSString *)other;
@end
@interface NSData : NSObject
@property(readonly) NSUInteger length;
@property(readonly) const void *bytes NS_RETURNS_INNER_POINTER;
+ (instancetype)dataWithBytes:(nullable const void *)bytes length:(NSUInteger)length;
@end
@interface NSArray<__covariant ObjectType> : NSObject <NSFastEnumeration>
@property(readonly) NSUInteger count;
- (BOOL)containsObject:(ObjectType)anObject;
@end
@interface NSMutableArray<ObjectType> : NSArray <ObjectType>
+ (instancetype)array;
- (void)addObject:(ObjectType)anObject;
- (void)removeObject:(ObjectType)anObject;
@end
@interface NSDictionary<KeyType, ObjectType> : NSObject
+ (instancetype)dictionaryWithObjects:(const ObjectType _Nonnull[_Nullable])objects forKeys:(const KeyType _Nonnull[_Nullable])keys count:(NSUInteger)cnt;
@end
@interface NSDate : NSObject
+ (instancetype)dateWithTimeIntervalSinceNow:(NSTimeInterval)secs;
@property(readonly) NSTimeInterval timeIntervalSinceNow;
@end
@interface NSThread : NSObject
@property(class, readonly) BOOL isMainThread;
+ (void)sleepForTimeInterval:(NSTimeInterval)ti;
@end
@interface NSError : NSObject
@property(readonly) NSInteger code;
@property(readonly, copy) NSString *localizedDescription;
@end
@interface NSException : NSObject
@end
// CoreFoundation run loop (CFRunLoop.h)
typedef const struct __attribute__((objc_bridge(NSString))) __CFString *CFStringRef;
typedef CFStringRef CFRunLoopMode;
typedef unsigned char Boolean;
typedef double CFTimeInterval;
typedef int32_t CFRunLoopRunResult;
extern const CFRunLoopMode kCFRunLoopDefaultMode;
CFRunLoopRunResult CFRunLoopRunInMode(CFRunLoopMode mode, CFTimeInterval seconds, Boolean returnAfterSourceHandled);
NS_ASSUME_NONNULL_END
