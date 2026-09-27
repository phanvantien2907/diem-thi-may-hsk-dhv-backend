import http from 'k6/http';
import { check, sleep } from 'k6';
import exec from 'k6/execution';
import { SharedArray } from 'k6/data';
import crypto from 'k6/crypto';

// Đọc 100,000 user đã setup
const users = new SharedArray('users', function () {
    return JSON.parse(open('./flashsale_users.json'));
});

const checksumKey = "16daddd823fb554d17fc26bb8a769d20ef30620eaa546c1a0398a724cd5be90b";

export const options = {
    scenarios: {
        payment_breaking_point: {
            executor: 'ramping-arrival-rate',
            startRate: 100,      // Bắt đầu với 100 RPS
            timeUnit: '1s',
            preAllocatedVUs: 500, // Cấp sẵn 500 VUs
            maxVUs: 3000,         // Cho phép tăng lên tối đa 3000 VUs để đẩy tải
            stages: [
                { target: 100, duration: '5s' },   // Khởi động nhẹ
                { target: 500, duration: '10s' },  // Tăng lên 500 RPS
                { target: 1000, duration: '10s' }, // Tăng lên 1000 RPS
                { target: 1500, duration: '10s' }, // Tăng lên 1500 RPS
                { target: 2000, duration: '10s' }, // Ép tải 2000 RPS
                { target: 3000, duration: '10s' }, // Vượt giới hạn 3000 RPS
                { target: 4000, duration: '10s' }, // Xem server có crash không
            ],
        },
    },
};

export default function () {
    // 1. Pick a random user from 100k
    const randomUser = users[Math.floor(Math.random() * users.length)];
    const headers = {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${randomUser.token}`,
    };

    // 2. Register (để có data thanh toán)
    let bookRes = http.post('http://localhost:8080/api/v1/me/registrations', JSON.stringify({
        exam_session_id: randomUser.exam_session_id
    }), { headers: headers });

    // Chỉ test Create Payment nếu đăng ký thành công
    if (bookRes.status === 201) {
        const body = JSON.parse(bookRes.body);
        const registrationId = body.data.id;

        // 3. Gọi Create Payment Link
        let payRes = http.post('http://localhost:8080/api/v1/payments/payos/create', JSON.stringify({
            exam_registration_id: registrationId
        }), { headers: headers, timeout: '10s' });

        check(payRes, {
            'create payment is 200/201': (r) => r.status === 200 || r.status === 201,
        });

        if (payRes.status === 201) {
            const payData = JSON.parse(payRes.body);
            const orderCode = payData.data.payment_id;

            // 4. (Tùy chọn) Bắn Webhook mô phỏng PayOS trả về
            let webhookData = {
                orderCode: orderCode,
                amount: 500000,
                reference: "R" + orderCode,
                accountNumber: "123", transactionDateTime: "2026",
                currency: "VND", paymentLinkId: "L", code: "00", desc: "success",
                counterAccountBankId: "B", counterAccountBankName: "B", counterAccountName: "C",
                counterAccountNumber: "1", virtualAccountName: "V", virtualAccountNumber: "1"
            };
            const keys = Object.keys(webhookData).sort();
            let sigParts = [];
            keys.forEach(k => sigParts.push(`${k}=${webhookData[k]}`));
            const signature = crypto.hmac('sha256', checksumKey, sigParts.join('&'), 'hex');

            let payload = JSON.stringify({
                code: "00", success: true, data: webhookData, signature: signature
            });

            let whRes = http.post('http://localhost:8080/api/v1/payments/callback', payload, {
                headers: { 'Content-Type': 'application/json' },
                timeout: '5s'
            });

            check(whRes, {
                'webhook status is 200': (r) => r.status === 200,
            });
        }
    }
}
