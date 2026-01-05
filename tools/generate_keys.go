package main

import (
	"bufio"
	"crypto/rand"
	"fmt"
	"math/big"
	"os"
)

func main() {
	// 生成卡密并写入文件
	f, err := os.Create("license_keys.txt")
	if err != nil {
		fmt.Printf("创建文件失败: %v\n", err)
		return
	}
	defer f.Close()

	writer := bufio.NewWriter(f)

	// 7天体验卡密（100个）
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("7D-%s", generateRandomKey(10))
		fmt.Fprintln(writer, key)
	}

	// 1个月月卡卡密（100个）
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("1M-%s", generateRandomKey(10))
		fmt.Fprintln(writer, key)
	}

	// 1年年卡卡密（100个）
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("1Y-%s", generateRandomKey(10))
		fmt.Fprintln(writer, key)
	}

	writer.Flush()

	fmt.Println("✅ 卡密已生成到文件: license_keys.txt")
	fmt.Println("   共生成 300 个卡密（100个7天、100个1个月、100个1年）")
	fmt.Println("   请将此文件放到 backend/license/ 目录下")
}

func generateRandomKey(length int) string {
	const charset = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // 去除容易混淆的字符：0/O, I/1
	b := make([]byte, length)
	for i := range b {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		b[i] = charset[n.Int64()]
	}
	if length == 10 {
		return string(b[:4]) + "-" + string(b[4:7]) + "-" + string(b[7:])
	}
	return string(b)
}
