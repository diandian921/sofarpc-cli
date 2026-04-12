# SofaRPC CLI — Skill

## 工具调用方式
sofarpc <命令>   （需已完成安装，见安装说明）

## 环境要求
- JDK 8+
- 网络可达目标服务

## 配置目录
~/.sofarpc/servers.yaml   ← 服务地址
~/.sofarpc/config.yaml    ← 全局配置

## 命令速查

### 服务地址管理
sofarpc server list                                    # 查看全部
sofarpc server list <keyword>                          # 模糊搜索
sofarpc server add <n> <ip:port> [--desc <备注>]       # 添加
sofarpc server remove <n>                              # 删除
sofarpc server export > servers.yaml                   # 导出给同事
sofarpc server import servers.yaml                     # 从同事处导入

### 冒烟测试
sofarpc ping --server <n> --service <接口全限定类名>
Exit: 0=UP 1=DOWN 2=连接失败 4=别名不存在

### 单次调用（单参数）
sofarpc invoke \
  --server <n> \
  --service <接口全限定类名> \
  --method <方法名> \
  --arg-types '<参数全限定类名>' \
  --args '<json对象>' \
  --assert '<JSONPath表达式>' \
  --json

### 单次调用（多参数）
sofarpc invoke \
  --server <n> \
  --service <接口全限定类名> \
  --method <方法名> \
  --arg-types '<类型1>,<类型2>' \
  --args '[值1, 值2]' \
  --json

### 批量测试
sofarpc batch --server <n> --cases <dir> --json > result.json

### 生成报告
sofarpc report --input result.json --format markdown

## 标准工作流
1. server list 确认目标服务已配置
2. ping 确认服务存活
3. invoke 验证单个接口（建议带 --assert 做返回值校验）
4. batch 批量跑用例目录
5. report 生成测试报告

## 注意事项
- 所有调用必须带 --json，保证输出可解析
- --args 必须是合法 JSON
- --arg-types 在有 --args 时必须指定，无参方法可省略
- 正常输出到 stdout，错误信息到 stderr
- batch 结果建议重定向到文件再生成报告
