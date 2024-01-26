# 数据平台

## 程序启动

### 启动EdgeX Foundry

运行`Docker Desktop`程序，数据平台的程序依赖`Linux`环境，数据平台的主体部分运行在`Docker`中（`Device-Service`、`App-Service`除外）

![image-20240126192654696](C:\Users\legion\AppData\Roaming\Typora\typora-user-images\image-20240126192654696.png)

运行`EdgeX-Compose`文件夹中的`StartDocker.sh`脚本，用以启动数据平台主体程序，通过`Docker-Compose`启动：

<img src="C:\Users\legion\AppData\Roaming\Typora\typora-user-images\image-20240126192933256.png" alt="image-20240126192933256" style="zoom:67%;" />

程序正常运行如下图，一共**11个容器均为Running状态**则启动正常：

<img src="C:\Users\legion\AppData\Roaming\Typora\typora-user-images\image-20240126193050367.png" alt="image-20240126193050367"  />

如果部分容器没有正常启动（显示橙色图标），可能是端口占用问题，重启容器无法解决。**使用管理员权限打开命令行窗口，输入如下命令并重启电脑**：

```bash
netsh winsock reset
```

<img src="C:\Users\legion\AppData\Roaming\Typora\typora-user-images\image-20240126193701651.png" alt="image-20240126193701651" style="zoom: 80%;" />

### 启动DeviceService

#### 修改配置文件

1. 通过命令行窗口，输入`ipconfig`命令确认数据平台的IP地址

<img src="C:\Users\legion\AppData\Roaming\Typora\typora-user-images\image-20240126192302293.png" alt="image-20240126192302293" style="zoom: 67%;" />

2. 将`Device-Service`中的配置文件`device-cccccf\cmd\device-cccccf\res\configuration.toml`的`Host`字段设置为主机IP，如上则为`192.168.31.70`

![image-20240126195052729](C:\Users\legion\AppData\Roaming\Typora\typora-user-images\image-20240126195052729.png)

3. **保存配置文件**

#### 运行程序

1. 在`device-cccccf`文件夹下，右键空白处，点击`Git Bash Here`，打开命令行窗口（Linux环境）：

![image-20240126194748308](C:\Users\legion\AppData\Roaming\Typora\typora-user-images\image-20240126194748308.png)

2. 输入如下命令，设置环境变量。**每次打开Git Bash均需要设置该环境变量**：

```bash
export EDGEX_SECURITY_SECRET_STORE=false
```

3. 进入到`cmd/device-cccccf/`文件夹，执行`./device-cccccf`命令运行`Device-Service`，成功运行打印如下：

![image-20240126195226406](C:\Users\legion\AppData\Roaming\Typora\typora-user-images\image-20240126195226406.png)

### 启动APP Service

#### 修改配置文件

1. 将`App-Service`中的配置文件`res\configuration.toml`的`Host`字段设置为主机IP，如上则为`192.168.31.70`

   <img src="C:\Users\legion\AppData\Roaming\Typora\typora-user-images\image-20240126195453280.png" alt="image-20240126195453280"  />

2. **修改功能开关、边缘设备服务器配置、云服务器配置**：

   + 功能开关：

     + EncryptImageData：是否加密面部表情（眼球追踪）数据
     + EncryptEEGData：是否加密脑电数据
     + ProcessFacialAndEEGData：是否处理面部表情数据
     + ProcessEyeTrackingData：是否处理眼球追踪数据
     + ProcessKeyboardAndMouseData：是否处理键鼠数据
     + SaveDataToCloud：是否将数据存储到云服务器

     **注：如果打开了某项功能开关，则必须将其相关服务的IP以及端口号配置正确**

   + 边缘设备服务器配置：

     + url：IP地址 + : + 端口号（FacialAndEEGModel - 50051，EyeTrackingModel - 50052，KeyBoardAndMouseModel - 50053，ImageEncryptionServer - 50054，EEGEncryptionServer - 50055）
     + Timeout：超时限制
     + Protocol：协议名称（grpc）

     **注：一般情况下只需要更改IP地址**

   + 云服务器配置（DatabaseServer）：

     + Url：http:// + IP地址 + : + 端口号（云服务器后端端口号 - 8080）
     + Timeout：超时限制
     + Protocol：协议名称（http）

     **注：一般情况下只需要更改IP地址**

   ![image-20240126195848033](C:\Users\legion\AppData\Roaming\Typora\typora-user-images\image-20240126195848033.png)

3. **保存配置文件**

#### 运行程序

1. 在`app-cccccf`文件夹下，右键空白处，点击`Git Bash Here`，打开命令行窗口（Linux环境）：

![image-20240126200936574](C:\Users\legion\AppData\Roaming\Typora\typora-user-images\image-20240126200936574.png)

2. 输入如下命令，设置环境变量。**每次打开Git Bash均需要设置该环境变量**：

```bash
export EDGEX_SECURITY_SECRET_STORE=false
```

3. 执行`./app-cccccf`命令运行`App-Service`，成功运行打印如下：

![image-20240126201017086](C:\Users\legion\AppData\Roaming\Typora\typora-user-images\image-20240126201017086.png)

## 运行报错分析

持续更新ing...