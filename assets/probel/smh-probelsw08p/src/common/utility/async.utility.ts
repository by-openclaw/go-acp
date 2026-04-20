import { Constants } from '../util/constants';

export abstract class AsyncUtility {
    protected constructor() {
        throw new Error(Constants.ABSTRACT_UTILITY_CLASS_CTOR_ERROR_MSG);
    }

    static timeoutOperationAsync(timeoutMs: number, thePromise: Promise<any>): Promise<any> {
        let timeoutId = <any>null;
        const timeoutPromise = new Promise((_, reject) => {
            timeoutId = setTimeout(() => reject(Error(`Operation time out after ${timeoutMs} ms`)), timeoutMs);
        });
        return Promise.race([timeoutPromise, thePromise]).finally(() => clearTimeout(timeoutId));
    }

    static delayAsync(timeoutMs: number): Promise<void> {
        return new Promise((resolve: any, reject: any) => {
            setTimeout(() => {
                resolve();
            }, timeoutMs);
        });
    }
}
