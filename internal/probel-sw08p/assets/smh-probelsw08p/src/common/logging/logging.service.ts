import { Service } from 'typedi';

export type LogMessage = () => string;

@Service()
export class LoggingService {
    private get currentDateTime(): string {
        return new Date().toISOString();
    }

    error(message: LogMessage, ...args: any[]): void {
        console.error(`${this.currentDateTime}|${this.error.name.toUpperCase()}|${message()}`, ...args);
    }

    warn(message: LogMessage, ...args: any[]): void {
        console.log(`${this.currentDateTime}|${this.warn.name.toUpperCase()}|${message()}`, ...args);
    }

    info(message: LogMessage, ...args: any[]): void {
        console.log(`${this.currentDateTime}|${this.info.name.toUpperCase()}|${message()}`, ...args);
    }

    debug(message: LogMessage, ...args: any[]): void {
        console.log(`${this.currentDateTime}|${this.debug.name.toUpperCase()}|${message()}`, ...args);
    }

    trace(message: LogMessage, ...args: any[]): void {
        console.log(`${this.currentDateTime}|${this.trace.name.toUpperCase()}|${message()}`, ...args);
    }
}
