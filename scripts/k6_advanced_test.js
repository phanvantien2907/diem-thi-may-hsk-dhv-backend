import http from 'k6/http';
import { check, sleep } from 'k6';
import crypto from 'k6/crypto';
import { SharedArray } from 'k6/data';

const users = new SharedArray('users', function () {
    return JSON.parse(open('./flashsale_users.json'));
});

const adminToken = users[0].token;
const checksumKey = "16daddd823fb554d17fc26bb8a769d20ef30620eaa546c1a0398a724cd5be90b";

export const options = {
    scenarios: {
        fake_webhook_injection: { executor: 'shared-iterations', vus: 10, iterations: 10, exec: 'fakeWebhookTest' },
        cronjob_clash: { executor: 'shared-iterations', vus: 2, iterations: 2, exec: 'cronjobClashTest', startTime: '2s' },
        out_of_order_webhook: { executor: 'shared-iterations', vus: 2, iterations: 2, exec: 'outOfOrderWebhookTest', startTime: '4s' },
        spam_refund: { executor: 'shared-iterations', vus: 10, iterations: 10, exec: 'spamRefundTest', startTime: '6s' },
        orphaned_webhook: { executor: 'shared-iterations', vus: 50, iterations: 100, exec: 'orphanedWebhookTest', startTime: '8s' },
        polling_storm: { executor: 'constant-vus', vus: 50, duration: '5s', exec: 'pollingStormTest', startTime: '10s' },
        webhook_after_cancel: { executor: 'shared-iterations', vus: 1, iterations: 1, exec: 'webhookAfterCancelTest', startTime: '16s' },
        data_injection_payload: { executor: 'shared-iterations', vus: 5, iterations: 5, exec: 'dataInjectionTest', startTime: '18s' },
        mass_cancellation: { executor: 'shared-iterations', vus: 50, iterations: 50, exec: 'massCancellationTest', startTime: '20s' },
        webhook_retry_burst: { executor: 'shared-iterations', vus: 5, iterations: 5, exec: 'webhookRetryBurstTest', startTime: '22s' },

        // --- NEW: Amount Mismatch (Underpaid/Overpaid) ---
        amount_mismatch: { executor: 'shared-iterations', vus: 2, iterations: 2, exec: 'amountMismatchTest', startTime: '24s' },
    },
};

export function setup() {
    let validCandidates = [];
    for (let i = 1; i < users.length; i++) {
        const token = users[i].token;
        const headers = { 'Authorization': 'Bearer ' + token };
        let getRes = http.get('http://localhost:8080/api/v1/me/registrations', { headers });
        if (getRes.status === 200) {
            let regList = JSON.parse(getRes.body);
            if (regList.data && regList.data.length > 0) {
                validCandidates.push({ token: token, regID: regList.data[0].id, accountID: users[i].account_id || 0 });
            }
        }
        if (validCandidates.length >= 20) break;
    }
    return { validCandidates };
}

function generateWebhookPayload(orderCode, amount, isSuccess, description = "K6-TEST") {
    const webhookData = {
        orderCode: orderCode, amount: amount, description: description,
        accountNumber: "123", reference: "REF", transactionDateTime: "2026",
        currency: "VND", paymentLinkId: "LINK", code: isSuccess ? "00" : "01", desc: isSuccess ? "success" : "failed",
        counterAccountBankId: "B", counterAccountBankName: "B", counterAccountName: "C",
        counterAccountNumber: "1", virtualAccountName: "V", virtualAccountNumber: "1"
    };
    const keys = Object.keys(webhookData).sort();
    let sigParts = [];
    keys.forEach(k => sigParts.push(`${k}=${webhookData[k]}`));
    const signature = crypto.hmac('sha256', checksumKey, sigParts.join('&'), 'hex');
    return JSON.stringify({ code: isSuccess ? "00" : "01", desc: isSuccess ? "success" : "failed", success: isSuccess, data: webhookData, signature: signature });
}

// ---------------------------------------------------------
// 1. Tấn công Giả mạo Chữ ký (Fake Webhook Injection)
// Kịch bản: Hacker tự tạo payload webhook với chữ ký (signature) giả mạo hòng cướp ghế thi mà không thanh toán.
// Kỳ vọng: API chặn đứng tại Middleware (trả về 401 Unauthorized), không lọt xuống DB.
export function fakeWebhookTest() {
    const signature = crypto.hmac('sha256', "i-am-a-hacker", "amount=1000&orderCode=999", 'hex');
    let res = http.post('http://localhost:8080/api/v1/payments/callback', JSON.stringify({ code: "00", success: true, data: {}, signature: signature }), { headers: { 'Content-Type': 'application/json' } });
    check(res, { 'Fake webhook bị block (401)': (r) => r.status === 401 });
}

// ---------------------------------------------------------
// 2. Xung đột Đồng thời (Cronjob Clash)
// Kịch bản: Admin/Cronjob gọi API Cancel đúng lúc Webhook Thành công (Success) vừa gửi tới cùng 1 mili-giây.
// Kỳ vọng: Nhờ Pessimistic Lock (FOR UPDATE) trên Database, 1 giao dịch bị chặn lại. Hệ thống không bị lỗi 500 hay sai lệch dữ liệu.
export function cronjobClashTest(data) {
    if (!data || data.validCandidates.length === 0) return;
    const candidate = data.validCandidates[0];
    let payRes = http.post('http://localhost:8080/api/v1/payments/payos/create', JSON.stringify({ exam_registration_id: candidate.regID }), { headers: { 'Content-Type': 'application/json', 'Authorization': 'Bearer ' + candidate.token } });
    if (payRes.status !== 201) return;
    const paymentData = JSON.parse(payRes.body);
    const paymentID = paymentData.data.payment_id;

    let responses = http.batch({
        cancelReq: { method: 'POST', url: `http://localhost:8080/api/v1/admin/payments/${paymentID}/cancel`, params: { headers: { 'Authorization': 'Bearer ' + adminToken } } },
        webhookReq: { method: 'POST', url: `http://localhost:8080/api/v1/payments/callback`, body: generateWebhookPayload(paymentID, paymentData.data.payment_amount, true), params: { headers: { 'Content-Type': 'application/json' } } }
    });
    check(responses.cancelReq, { 'Cancel response an toàn': (r) => r.status === 200 || r.status === 409 || r.status === 404 });
}

// ---------------------------------------------------------
// 3. Trạng thái Webhook Lộn xộn (Out-of-order Webhook)
// Kịch bản: Gateway mạng chập chờn bắn cả 2 tín hiệu "Failed" và "Success" ĐỒNG THỜI cho cùng 1 hóa đơn.
// Kỳ vọng: Hệ thống tự động Idempotent. Kẻ đến sau bị loại bỏ an toàn (trả về 200 OK cho PayOS để ngừng gửi).
export function outOfOrderWebhookTest(data) {
    if (!data || data.validCandidates.length < 2) return;
    const candidate = data.validCandidates[1];
    let payRes = http.post('http://localhost:8080/api/v1/payments/payos/create', JSON.stringify({ exam_registration_id: candidate.regID }), { headers: { 'Content-Type': 'application/json', 'Authorization': 'Bearer ' + candidate.token } });
    if (payRes.status !== 201) return;
    const paymentData = JSON.parse(payRes.body);

    let responses = http.batch({
        successReq: { method: 'POST', url: `http://localhost:8080/api/v1/payments/callback`, body: generateWebhookPayload(paymentData.data.payment_id, paymentData.data.payment_amount, true), params: { headers: { 'Content-Type': 'application/json' } } },
        failedReq: { method: 'POST', url: `http://localhost:8080/api/v1/payments/callback`, body: generateWebhookPayload(paymentData.data.payment_id, paymentData.data.payment_amount, false), params: { headers: { 'Content-Type': 'application/json' } } }
    });
    check(responses.successReq, { 'Out-of-order trả 200': (r) => r.status === 200 });
}

// ---------------------------------------------------------
// 4. Bào tiền Hệ thống (Spam hoàn tiền)
// Kịch bản: Kẻ gian spam click nút Hủy/Refund 10 lần liên tục hòng lợi dụng race condition để hoàn tiền nhiều lần.
// Kỳ vọng: Trả về HTTP 409 (Conflict) siêu tốc ngay từ request thứ 2, bảo vệ Database không bị sập.
export function spamRefundTest() {
    let res = http.post(`http://localhost:8080/api/v1/admin/payments/99999/cancel`, null, { headers: { 'Authorization': 'Bearer ' + adminToken } });
    check(res, { 'Spam hoàn tiền 404/409': (r) => r.status === 404 || r.status === 409 });
}

// ---------------------------------------------------------
// 5. Bão Webhook Mồ Côi (Orphaned Webhook)
// Kịch bản: Hàng loạt Webhook có mã OrderCode không hề tồn tại bị dội vào hệ thống hòng làm tràn DB (Full Table Scan).
// Kỳ vọng: Hệ thống từ chối truy vấn cực nhanh nhờ Index, trả 200 OK để giải phóng tải.
export function orphanedWebhookTest() {
    let res = http.post('http://localhost:8080/api/v1/payments/callback', generateWebhookPayload(Math.floor(Math.random() * 10000000) + 1000, 500000, true), { headers: { 'Content-Type': 'application/json' } });
    check(res, { 'Orphaned Webhook 200': (r) => r.status === 200 });
}

// ---------------------------------------------------------
// 6. Bão Truy Vấn Lịch Sử (Polling Storm)
// Kịch bản: Hàng ngàn thí sinh liên tục F5 trang web mỗi 500ms để đợi kết quả thanh toán.
// Kỳ vọng: API GET Lịch sử hoạt động độc lập (Read-path), không bị tranh chấp tài nguyên với Write-path.
export function pollingStormTest(data) {
    if (!data || data.validCandidates.length === 0) return;
    let res = http.get(`http://localhost:8080/api/v1/payments?candidate_id=${data.validCandidates[0].accountID}`, { headers: { 'Authorization': 'Bearer ' + data.validCandidates[0].token } });
    check(res, { 'Polling Lịch sử (200)': (r) => r.status === 200 });
    sleep(0.5);
}

// ---------------------------------------------------------
// 7. Hồi sinh Đơn rác (Webhook After Cancel / Delay Attack)
// Kịch bản: Admin đã hủy đơn, nhưng rất lâu sau Webhook từ PayOS báo Success mới bò về tới nơi (do mạng lag).
// Kỳ vọng: SP chặn cứng (chỉ nhận khi trạng thái là 'pending'), đơn rác không bị "hồi sinh".
export function webhookAfterCancelTest(data) {
    if (!data || data.validCandidates.length < 3) return;
    const candidate = data.validCandidates[2];
    let payRes = http.post('http://localhost:8080/api/v1/payments/payos/create', JSON.stringify({ exam_registration_id: candidate.regID }), { headers: { 'Content-Type': 'application/json', 'Authorization': 'Bearer ' + candidate.token } });
    if (payRes.status !== 201) return;
    const paymentData = JSON.parse(payRes.body);

    let cancelRes = http.post(`http://localhost:8080/api/v1/admin/payments/${paymentData.data.payment_id}/cancel`, null, { headers: { 'Authorization': 'Bearer ' + adminToken } });
    check(cancelRes, { 'Admin hủy đơn thành công': (r) => r.status === 200 });
    sleep(1);

    let webhookRes = http.post('http://localhost:8080/api/v1/payments/callback', generateWebhookPayload(paymentData.data.payment_id, paymentData.data.payment_amount, true), { headers: { 'Content-Type': 'application/json' } });
    check(webhookRes, { 'Webhook sau khi Hủy (200)': (r) => r.status === 200 });
}

// ---------------------------------------------------------
// 8. Tấn Công Nhồi Data (Data Injection / Payload Bomb)
// Kịch bản: Hacker chèn mã SQL Injection hoặc chuỗi rác khổng lồ (Emoji) vào description hòng đánh vỡ Data Types hoặc làm sập bảng Log.
// Kỳ vọng: ORM + JSONB vệ sinh dữ liệu mượt mà, lưu log không sót chữ nào và an toàn tuyệt đối.
export function dataInjectionTest() {
    let res = http.post('http://localhost:8080/api/v1/payments/callback', generateWebhookPayload(88888, 500000, true, "'); DROP TABLE payments; -- 🤪🚨💥"), { headers: { 'Content-Type': 'application/json' } });
    check(res, { 'Tấn công data injection (200)': (r) => r.status === 200 });
}

// ---------------------------------------------------------
// 9. Bão Hủy Đơn Diện Rộng (Mass Cancellation)
// Kịch bản: Hacker (hoặc Admin bối rối) bắn hàng chục ngàn request Hủy đơn vào các ID bất kỳ để vắt kiệt DB Connection Pool.
// Kỳ vọng: Hệ thống cản phá ngay lập tức (trả về 404/409 siêu tốc), không bị tắc nghẽn giao dịch.
export function massCancellationTest(data) {
    let cancelRes = http.post(`http://localhost:8080/api/v1/admin/payments/${Math.floor(Math.random() * 10000)}/cancel`, null, { headers: { 'Authorization': 'Bearer ' + adminToken } });
    check(cancelRes, { 'Bão hủy đơn hàng loạt (404/409)': (r) => r.status === 404 || r.status === 409 });
}

// ---------------------------------------------------------
// 10. Gateway Trả Đũa Cục Bộ (Gateway Retry Burst)
// Kịch bản: PayOS tưởng Backend chết nên bắn dồn dập 5 Webhook báo "Thành Công" (Giống y hệt nhau) trong cùng 1 Mili-giây.
// Kỳ vọng: Database Lock (FOR UPDATE) biến 5 đường thành 1 hàng dọc. Xử lý cái đầu tiên, 4 cái sau return -1, tránh được việc cộng nhầm tiền.
export function webhookRetryBurstTest(data) {
    if (!data || data.validCandidates.length < 5) return;
    const candidate = data.validCandidates[4];

    let payRes = http.post('http://localhost:8080/api/v1/payments/payos/create',
        JSON.stringify({ exam_registration_id: candidate.regID }), { headers: { 'Content-Type': 'application/json', 'Authorization': 'Bearer ' + candidate.token } });
    if (payRes.status !== 201) return;

    const paymentData = JSON.parse(payRes.body);
    const webhookPayload = generateWebhookPayload(paymentData.data.payment_id, paymentData.data.payment_amount, true);

    const reqs = {
        req1: { method: 'POST', url: `http://localhost:8080/api/v1/payments/callback`, body: webhookPayload, params: { headers: { 'Content-Type': 'application/json' } } },
        req2: { method: 'POST', url: `http://localhost:8080/api/v1/payments/callback`, body: webhookPayload, params: { headers: { 'Content-Type': 'application/json' } } },
        req3: { method: 'POST', url: `http://localhost:8080/api/v1/payments/callback`, body: webhookPayload, params: { headers: { 'Content-Type': 'application/json' } } },
        req4: { method: 'POST', url: `http://localhost:8080/api/v1/payments/callback`, body: webhookPayload, params: { headers: { 'Content-Type': 'application/json' } } },
        req5: { method: 'POST', url: `http://localhost:8080/api/v1/payments/callback`, body: webhookPayload, params: { headers: { 'Content-Type': 'application/json' } } },
    };

    let responses = http.batch(reqs);
    check(responses.req1, { 'Burst Webhook req1 (200)': (r) => r.status === 200 });
    check(responses.req5, { 'Burst Webhook req5 (200)': (r) => r.status === 200 });
}

// ---------------------------------------------------------
// 11. Thanh Toán Sai Số Tiền (Amount Mismatch / Underpaid)
// Kịch bản: Hacker tự sửa số tiền thanh toán (Ví dụ: Đơn 500k nhưng sửa thành 10k). PayOS vô tình duyệt và báo Webhook Success.
// Kỳ vọng: Database Guard `p_amount <> v_payment_amount` phát hiện lệch tiền và từ chối cập nhật (trả về 400 Bad Request).
export function amountMismatchTest(data) {
    if (!data || data.validCandidates.length === 0) return;
    // Tái sử dụng candidate 0 vì các test trước đó đã chạy xong ở giây 24
    const candidate = data.validCandidates[0];

    // 1. Tạo thanh toán gốc (Giả sử 500,000 VND)
    let payRes = http.post('http://localhost:8080/api/v1/payments/payos/create',
        JSON.stringify({ exam_registration_id: candidate.regID }), { headers: { 'Content-Type': 'application/json', 'Authorization': 'Bearer ' + candidate.token } });
    if (payRes.status !== 201) return;

    const paymentData = JSON.parse(payRes.body);

    // 2. K6 tạo Webhook giả lập việc thanh toán bị THIẾU 1 đồng (Underpaid)
    // Hacker không thể tự fake chữ ký nếu không có ChecksumKey, nhưng ở K6 ta dùng ChecksumKey thật để test DB Guard
    const fakeAmount = paymentData.data.payment_amount - 1;
    const webhookPayload = generateWebhookPayload(paymentData.data.payment_id, fakeAmount, true);

    let webhookRes = http.post('http://localhost:8080/api/v1/payments/callback', webhookPayload, { headers: { 'Content-Type': 'application/json' } });

    // Kỳ vọng: Hệ thống chặn đứng từ trong Database và ném lỗi 400 ra ngoài
    check(webhookRes, { 'Amount Mismatch (Thanh toán thiếu) bị block (400)': (r) => r.status === 400 });
}
