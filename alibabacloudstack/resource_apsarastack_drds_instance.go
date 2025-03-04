package alibabacloudstack

import (
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"

	"github.com/aliyun/alibaba-cloud-sdk-go/services/drds"
	"github.com/aliyun/terraform-provider-alibabacloudstack/alibabacloudstack/connectivity"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func resourceAlibabacloudStackDRDSInstance() *schema.Resource {
	return &schema.Resource{
		Create: resourceAliCloudDRDSInstanceCreate,
		Read:   resourceAliCloudDRDSInstanceRead,
		Update: resourceAliCloudDRDSInstanceUpdate,
		Delete: resourceAliCloudDRDSInstanceDelete,
		Importer: &schema.ResourceImporter{
			State: schema.ImportStatePassthrough,
		},

		Timeouts: &schema.ResourceTimeout{
			Create: schema.DefaultTimeout(10 * time.Minute),
			Update: schema.DefaultTimeout(5 * time.Minute),
			Delete: schema.DefaultTimeout(10 * time.Minute),
		},

		Schema: map[string]*schema.Schema{
			"description": {
				Type:         schema.TypeString,
				Required:     true,
				ValidateFunc: validation.StringLenBetween(1, 129),
			},
			"zone_id": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
			"specification": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
			"instance_charge_type": {
				Type:         schema.TypeString,
				Optional:     true,
				ValidateFunc: validation.StringInSlice([]string{string(PostPaid), string(PrePaid)}, false),
				ForceNew:     true,
				Default:      PostPaid,
			},
			"vswitch_id": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
			"instance_series": {
				Type:         schema.TypeString,
				Required:     true,
				ValidateFunc: validation.StringInSlice([]string{"drds.sn2.4c16g", "drds.sn2.8c32g", "drds.sn2.16c64g", "drds.sn1.32c64g"}, false),
				ForceNew:     true,
			},
		},
	}
}

func resourceAliCloudDRDSInstanceCreate(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*connectivity.AlibabacloudStackClient)
	drdsService := DrdsService{client}

	request := drds.CreateCreateDrdsInstanceRequest()
	request.RegionId = client.RegionId
	request.Description = d.Get("description").(string)
	//request.Type = "1"
	request.Type = "PRIVATE"
	request.ZoneId = d.Get("zone_id").(string)
	request.Specification = d.Get("specification").(string)
	request.PayType = d.Get("instance_charge_type").(string)
	request.VswitchId = d.Get("vswitch_id").(string)
	request.InstanceSeries = d.Get("instance_series").(string)
	request.Quantity = "1"

	if request.VswitchId != "" {

		vpcService := VpcService{client}
		vsw, err := vpcService.DescribeVSwitch(request.VswitchId)
		if err != nil {
			return WrapError(err)
		}

		request.VpcId = vsw.VpcId
	}
	request.ClientToken = buildClientToken(request.GetActionName())

	if request.PayType == string(PostPaid) {
		request.PayType = "drdsPost"
	}
	if request.PayType == string(PrePaid) {
		request.PayType = "drdsPre"
	}
	request.ResourceGroupId = client.Department

	request.Headers["x-ascm-product-name"] = "Drds"
	request.Headers["x-acs-organizationId"] = client.Department
	raw, err := client.WithDrdsClient(func(drdsClient *drds.Client) (interface{}, error) {
		return drdsClient.CreateDrdsInstance(request)
	})
	if err != nil {
		return WrapErrorf(err, DefaultErrorMsg, "alibabacloudstack_drds_instance", request.GetActionName(), AlibabacloudStackSdkGoERROR)
	}
	addDebug(request.GetActionName(), raw, request.RpcRequest, request)
	response, _ := raw.(*drds.CreateDrdsInstanceResponse)
	idList := response.Data.DrdsInstanceIdList.DrdsInstanceId
	if len(idList) != 1 {
		return WrapError(Error("failed to get DRDS instance id and response. DrdsInstanceIdList is %#v", idList))
	}
	d.SetId(idList[0])

	// wait instance status change from DO_CREATE to RUN
	stateConf := BuildStateConf([]string{"DO_CREATE"}, []string{"RUN"}, d.Timeout(schema.TimeoutCreate), 1*time.Minute, drdsService.DrdsInstanceStateRefreshFunc(d.Id(), []string{}))
	if _, err := stateConf.WaitForState(); err != nil {
		return WrapErrorf(err, IdMsg, d.Id())
	}

	return resourceAliCloudDRDSInstanceUpdate(d, meta)

}

func resourceAliCloudDRDSInstanceUpdate(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*connectivity.AlibabacloudStackClient)
	drdsService := DrdsService{client}

	configItem := make(map[string]string)
	if d.HasChange("description") {
		request := drds.CreateModifyDrdsInstanceDescriptionRequest()
		request.DrdsInstanceId = d.Id()
		request.Description = d.Get("description").(string)
		configItem["description"] = request.Description
		client := meta.(*connectivity.AlibabacloudStackClient)
		request.RegionId = client.RegionId
		request.Headers["x-ascm-product-name"] = "Drds"
		request.Headers["x-acs-organizationId"] = client.Department
		raw, err := client.WithDrdsClient(func(drdsClient *drds.Client) (interface{}, error) {
			return drdsClient.ModifyDrdsInstanceDescription(request)
		})
		if err != nil {
			return WrapErrorf(err, DefaultErrorMsg, d.Id(), request.GetActionName(), AlibabacloudStackSdkGoERROR)
		}
		addDebug(request.GetActionName(), raw, request.RpcRequest, request)
	}

	//wait for update effected and instance status returning to run
	if err := drdsService.WaitDrdsInstanceConfigEffect(
		d.Id(), configItem, d.Timeout(schema.TimeoutUpdate)); err != nil {
		return WrapError(err)
	}
	stateConf := BuildStateConf([]string{}, []string{"RUN"}, d.Timeout(schema.TimeoutUpdate), 3*time.Second, drdsService.DrdsInstanceStateRefreshFunc(d.Id(), []string{}))
	if _, err := stateConf.WaitForState(); err != nil {
		return WrapErrorf(err, IdMsg, d.Id())
	}

	return resourceAliCloudDRDSInstanceRead(d, meta)
}

func resourceAliCloudDRDSInstanceRead(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*connectivity.AlibabacloudStackClient)
	drdsService := DrdsService{client}

	object, err := drdsService.DescribeDrdsInstance(d.Id())
	if err != nil {
		if NotFoundError(err) {
			d.SetId("")
			return nil
		}
		return WrapError(err)
	}
	data := object.Data
	//other attribute not set,because these attribute from `data` can't  get
	d.Set("zone_id", data.ZoneId)
	d.Set("description", data.Description)

	return nil
}

func resourceAliCloudDRDSInstanceDelete(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*connectivity.AlibabacloudStackClient)
	drdsService := DrdsService{client}
	request := drds.CreateRemoveDrdsInstanceRequest()
	request.RegionId = client.RegionId
	request.DrdsInstanceId = d.Id()

	request.Headers["x-ascm-product-name"] = "Drds"
	request.Headers["x-acs-organizationId"] = client.Department
	raw, err := client.WithDrdsClient(func(drdsClient *drds.Client) (interface{}, error) {
		return drdsClient.RemoveDrdsInstance(request)
	})
	if err != nil {
		if IsExpectedErrors(err, []string{"InvalidDrdsInstanceId.NotFound"}) {
			return nil
		}
		return WrapErrorf(err, DefaultErrorMsg, d.Id(), request.GetActionName(), AlibabacloudStackSdkGoERROR)
	}
	addDebug(request.GetActionName(), raw, request.RpcRequest, request)
	response, _ := raw.(*drds.RemoveDrdsInstanceResponse)

	if !response.Success {
		return WrapError(Error("failed to delete instance timeout "+"and got an error: %#v", err))
	}

	//0 -> RUN, 1->DO_CREATE, 2->EXCEPTION, 3->EXPIRE, 4->DO_RELEASE, 5->RELEASE, 6->UPGRADE, 7->DOWNGRADE, 10->VersionUpgrade, 11->VersionRollback, 14->RESTART
	stateConf := BuildStateConf([]string{"RUN", "DO_CREATE", "EXCEPTION", "EXPIRE", "DO_RELEASE", "RELEASE", "UPGRADE", "DOWNGRADE", "VersionUpgrade", "VersionRollback", "RESTART"}, []string{}, d.Timeout(schema.TimeoutDelete), 3*time.Second, drdsService.DrdsInstanceStateRefreshFunc(d.Id(), []string{}))
	if _, err = stateConf.WaitForState(); err != nil {
		return WrapErrorf(err, IdMsg, d.Id())
	}
	return nil
}
