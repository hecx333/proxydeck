import axios from "axios";
import { message } from "antd";

type RequestMeta = {
  silenceError?: boolean;
};

const client = axios.create({
  baseURL: import.meta.env.VITE_API_BASE_URL ?? "/api",
  withCredentials: true
});

client.interceptors.response.use(
  (response) => response,
  (error) => {
    const config = error.config as RequestMeta | undefined;
    const status = error.response?.status;
    const detail = error.response?.data?.error;
    if (error.response?.status === 401 && window.location.pathname !== "/login") {
      window.location.href = "/login";
    }
    if (!config?.silenceError && status !== 401) {
      message.error(detail || error.message || "Request failed");
    }
    return Promise.reject(error);
  }
);

export default client;
