import { Service } from 'typedi';

import { LoggingService } from '../logging/logging.service';
import { Package } from './package.model';

/**
 * Provide access to the 'package.json' file as a 'Package' object
 *
 * @export
 * @class PackageService
 */
@Service()
export class PackageService {
    /**
     * Creates an instance of PackageService
     *
     * @param {LoggingService} _loggingService the logging service
     * @memberof PackageService
     */
    constructor(private _loggingService: LoggingService) {
        this._loggingService.trace(() => `${PackageService.name}::ctor`);
    }

    /**
     * Gets the 'package.json' file as a 'Package' object
     *
     * @readonly
     * @type {Package}
     * @memberof PackageService
     */
    get package(): Package {
        return new Package('../../../package.json');
    }
}
