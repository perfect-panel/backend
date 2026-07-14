package user

import (
	"time"

	"github.com/perfect-panel/server/internal/model/entity/subscribe"
	"gorm.io/gorm"
)

type User struct {
	Id                    int64          `gorm:"primaryKey"`
	Password              string         `gorm:"type:varchar(100);not null;comment:User Password"`
	Algo                  string         `gorm:"type:varchar(20);default:'default';comment:Encryption Algorithm"`
	Salt                  string         `gorm:"type:varchar(20);default:null;comment:Password Salt"`
	Avatar                string         `gorm:"type:MEDIUMTEXT;comment:User Avatar"`
	Balance               int64          `gorm:"default:0;comment:User Balance"` // User Balance Amount
	ReferCode             string         `gorm:"type:varchar(20);default:'';comment:Referral Code"`
	RefererId             int64          `gorm:"index:idx_referer;comment:Referrer ID"`
	Commission            int64          `gorm:"default:0;comment:Commission"`                      // Commission Amount
	ReferralPercentage    uint8          `gorm:"default:0;comment:Referral"`                        // Referral Percentage
	OnlyFirstPurchase     *bool          `gorm:"default:true;not null;comment:Only First Purchase"` // Only First Purchase Referral
	GiftAmount            int64          `gorm:"default:0;comment:User Gift Amount"`
	Enable                *bool          `gorm:"default:true;not null;comment:Is Account Enabled"`
	IsAdmin               *bool          `gorm:"default:false;not null;comment:Is Admin"`
	EnableBalanceNotify   *bool          `gorm:"default:false;not null;comment:Enable Balance Change Notifications"`
	EnableLoginNotify     *bool          `gorm:"default:false;not null;comment:Enable Login Notifications"`
	EnableSubscribeNotify *bool          `gorm:"default:false;not null;comment:Enable Subscription Notifications"`
	EnableTradeNotify     *bool          `gorm:"default:false;not null;comment:Enable Trade Notifications"`
	AuthMethods           []AuthMethods  `gorm:"foreignKey:UserId;references:Id"`
	UserDevices           []Device       `gorm:"foreignKey:UserId;references:Id"`
	Rules                 string         `gorm:"type:TEXT;comment:User Rules"`
	CreatedAt             time.Time      `gorm:"<-:create;comment:Creation Time"`
	UpdatedAt             time.Time      `gorm:"comment:Update Time"`
	DeletedAt             gorm.DeletedAt `gorm:"index;comment:Deletion Time"`
}

func (*User) TableName() string {
	return "user"
}

type Subscribe struct {
	Id          int64      `gorm:"primaryKey"`
	UserId      int64      `gorm:"index:idx_user_id;not null;comment:User ID"`
	User        User       `gorm:"foreignKey:UserId;references:Id"`
	OrderId     int64      `gorm:"index:idx_order_id;not null;comment:Order ID"`
	SubscribeId int64      `gorm:"index:idx_subscribe_id;not null;comment:Subscription ID"`
	StartTime   time.Time  `gorm:"default:CURRENT_TIMESTAMP(3);not null;comment:Subscription Start Time"`
	ExpireTime  time.Time  `gorm:"default:NULL;comment:Subscription Expire Time"`
	FinishedAt  *time.Time `gorm:"default:NULL;comment:Finished Time"`
	Traffic     int64      `gorm:"default:0;comment:Traffic"`
	Download    int64      `gorm:"default:0;comment:Download Traffic"`
	Upload      int64      `gorm:"default:0;comment:Upload Traffic"`
	Token       string     `gorm:"index:idx_token;unique;type:varchar(255);default:'';comment:Token"`
	UUID        string     `gorm:"type:varchar(255);unique;index:idx_uuid;default:'';comment:UUID"`
	Status      uint8      `gorm:"type:tinyint(1);default:0;comment:Subscription Status: 0: Pending 1: Active 2: Finished 3: Expired 4: Deducted 5: stopped"`
	Note        string     `gorm:"type:varchar(500);default:'';comment:User note for subscription"`
	CreatedAt   time.Time  `gorm:"<-:create;comment:Creation Time"`
	UpdatedAt   time.Time  `gorm:"comment:Update Time"`
}

func (*Subscribe) TableName() string {
	return "user_subscribe"
}

type AuthMethods struct {
	Id             int64     `gorm:"primaryKey"`
	UserId         int64     `gorm:"index:idx_user_id;not null;comment:User ID"`
	AuthType       string    `gorm:"type:varchar(255);not null;comment:Auth Type 1: apple 2: google 3: github 4: facebook 5: telegram 6: email 7: mobile 8: device"`
	AuthIdentifier string    `gorm:"type:varchar(255);unique;index:idx_auth_identifier;not null;comment:Auth Identifier"`
	Verified       bool      `gorm:"default:false;not null;comment:Is Verified"`
	CreatedAt      time.Time `gorm:"<-:create;comment:Creation Time"`
	UpdatedAt      time.Time `gorm:"comment:Update Time"`
}

func (*AuthMethods) TableName() string {
	return "user_auth_methods"
}

type Device struct {
	Id         int64     `gorm:"primaryKey"`
	Ip         string    `gorm:"type:varchar(255);not null;comment:Device IP"`
	UserId     int64     `gorm:"index:idx_user_id;not null;comment:User ID"`
	UserAgent  string    `gorm:"default:null;comment:UserAgent."`
	Identifier string    `gorm:"type:varchar(255);unique;index:idx_identifier;default:'';comment:Device Identifier"`
	Online     bool      `gorm:"default:false;not null;comment:Online"`
	Enabled    bool      `gorm:"default:true;not null;comment:Enabled"`
	CreatedAt  time.Time `gorm:"<-:create;comment:Creation Time"`
	UpdatedAt  time.Time `gorm:"comment:Update Time"`
}

func (*Device) TableName() string {
	return "user_device"
}

type DeviceOnlineRecord struct {
	Id            int64     `gorm:"primaryKey"`
	UserId        int64     `gorm:"type:bigint;not null;comment:User ID"`
	Identifier    string    `gorm:"type:varchar(255);not null;comment:Device Identifier"`
	OnlineTime    time.Time `gorm:"comment:Online Time"` // The time when the device goes online
	OfflineTime   time.Time `gorm:"comment:Offline Time"`
	OnlineSeconds int64     `gorm:"comment:Offline Seconds"`
	DurationDays  int64     `gorm:"comment:Duration Days"`
	CreatedAt     time.Time `gorm:"<-:create;comment:Creation Time"`
}

func (DeviceOnlineRecord) TableName() string {
	return "user_device_online_record"
}

type Withdrawal struct {
	Id        int64     `gorm:"primaryKey"`
	UserId    int64     `gorm:"index:idx_user_id;not null;comment:User ID"`
	Amount    int64     `gorm:"not null;comment:Withdrawal Amount"`
	Content   string    `gorm:"type:text;comment:Withdrawal Content"`
	Status    uint8     `gorm:"type:tinyint(1);default:0;comment:Withdrawal Status: 0: Pending 1: Approved 2: Rejected"`
	Reason    string    `gorm:"type:varchar(500);default:'';comment:Rejection Reason"`
	CreatedAt time.Time `gorm:"<-:create;comment:Creation Time"`
	UpdatedAt time.Time `gorm:"comment:Update Time"`
}

func (*Withdrawal) TableName() string {
	return "user_withdrawal"
}

// SubscribeDetails is the joined view of a user subscription with its subscribe plan.
type SubscribeDetails struct {
	Id          int64                `gorm:"primarykey"`
	UserId      int64                `gorm:"index:idx_user_id;not null;comment:User ID"`
	User        *User                `gorm:"foreignKey:UserId;references:Id"`
	OrderId     int64                `gorm:"index:idx_order_id;not null;comment:Order ID"`
	SubscribeId int64                `gorm:"index:idx_subscribe_id;not null;comment:Subscription ID"`
	Subscribe   *subscribe.Subscribe `gorm:"foreignKey:SubscribeId;references:Id"`
	StartTime   time.Time            `gorm:"default:CURRENT_TIMESTAMP(3);not null;comment:Subscription Start Time"`
	ExpireTime  time.Time            `gorm:"default:NULL;comment:Subscription Expire Time"`
	FinishedAt  *time.Time           `gorm:"default:NULL;comment:Finished Time"`
	Traffic     int64                `gorm:"default:0;comment:Traffic"`
	Download    int64                `gorm:"default:0;comment:Download Traffic"`
	Upload      int64                `gorm:"default:0;comment:Upload Traffic"`
	Token       string               `gorm:"index:idx_token;unique;type:varchar(255);default:'';comment:Token"`
	UUID        string               `gorm:"type:varchar(255);unique;index:idx_uuid;default:'';comment:UUID"`
	Status      uint8                `gorm:"type:tinyint(1);default:0;comment:Subscription Status: 0: Pending 1: Active 2: Finished 3: Expired; 4: Cancelled"`
	Note        string               `gorm:"type:varchar(500);default:'';comment:User note for subscription"`
	CreatedAt   time.Time            `gorm:"<-:create;comment:Creation Time"`
	UpdatedAt   time.Time            `gorm:"comment:Update Time"`
}

// SubscribeLogFilterParams filters user subscribe access logs.
type SubscribeLogFilterParams struct {
	IP              string
	UserAgent       string
	UserId          int64
	Token           string
	UserSubscribeId int64
}

// LoginLogFilterParams filters user login logs.
type LoginLogFilterParams struct {
	IP        string
	UserId    int64
	UserAgent string
	Success   *bool
}

// UserFilterParams filters users in paginated queries.
type UserFilterParams struct {
	Search             string
	UserId             *int64
	SubscribeId        *int64
	UserSubscribeId    *int64
	UserSubscribeToken string
	Order              string // Order by id, e.g., "desc"
	Unscoped           bool   // Whether to include soft-deleted records
}

// EmailRecipientFilter filters email recipients.
type EmailRecipientFilter struct {
	Scope             int8
	RegisterStartTime int64
	RegisterEndTime   int64
}

// SubscribeFilter filters user subscriptions.
type SubscribeFilter struct {
	Subscribers []int64
	IsActive    *bool
	StartTime   int64
	EndTime     int64
}

// UserStatisticsWithDate holds aggregated user statistics per day/month.
type UserStatisticsWithDate struct {
	Date              string
	Register          int64
	NewOrderUsers     int64
	RenewalOrderUsers int64
}
