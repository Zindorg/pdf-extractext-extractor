import http from 'k6/http';
import { Trend } from 'k6/metrics';
import { check, sleep } from 'k6';

const statusTrend = new Trend('status_codes');

export const options = {
    stages: [
        { duration: '10s', target: 100 },
        { duration: '20s', target: 100 },
        { duration: '10s', target: 0 },
    ],
    thresholds: {
        http_req_failed: ['rate==0'],
        http_req_duration: ['max<15000'],
    },
    discardResponseBodies: true,
};

const BASE_URL = 'https://extract.universidad.localhost';

// Carga de PDFs en modo binario durante la inicialización (init context de k6)
const pdfFiles = [
    open('./pdfs/2020-Scrum-Guide-Spanish-Latin-South-American.pdf', 'b'),
    open('./pdfs/Essential-Kanban-Condensed-v1.0.0.pdf', 'b'),
    open('./pdfs/978-84-11920-35-3 Filosofia Lean.pdf', 'b'),
    open('./pdfs/historias_usuario_scrum_manager.pdf', 'b'),
];

export default function () {
    // Selección aleatoria de un PDF de la lista
    const randomPdf = pdfFiles[Math.floor(Math.random() * pdfFiles.length)];

    const params = {
        headers: {
            'Content-Type': 'application/pdf',
        },
    };

    const res = http.post(`${BASE_URL}/extract`, randomPdf, params);

    statusTrend.add(res.status);

    check(res, {
        'status 200': (r) => r.status === 200,
    });
}


