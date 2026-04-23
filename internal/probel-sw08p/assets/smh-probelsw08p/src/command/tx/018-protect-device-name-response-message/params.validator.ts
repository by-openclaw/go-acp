import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { IValidator } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { ProtectDeviceNameResponseCommandParams } from './params';

/**
 * ProtectDeviceNameResponseMessage command parameters validator
 *
 * @export
 * @class CommandParamsValidator
 * @implements {IValidator<ProtectDeviceNameResponseCommandParams>}
 */
export class CommandParamsValidator implements IValidator<ProtectDeviceNameResponseCommandParams> {
    /**
     * Creates an instance of CommandParamsValidator
     *
     * @param {ProtectDeviceNameResponseCommandParams} data the command parameters
     * @memberof CommandParamsValidator
     */
    constructor(readonly data: ProtectDeviceNameResponseCommandParams) {}

    /**
     * Validates the command parameter(s) and returns one error message for each invalid parameter
     *
     * @returns {Record<string, LocaleData>} the localized validation errors
     * @memberof CommandParamsValidator
     */
    validate(): Record<string, LocaleData> {
        const errors: Record<string, any> = {};
        const cache = LocaleDataCache.INSTANCE;

        if (this.isDeviceIdOutOfRange()) {
            errors.deviceId = cache.getCommandErrorLocaleData(CommandErrorsKeys.DEVICE_IS_OUT_OF_RANGE_ERROR_MSG);
        }
        if (this.isDeviceNameOutOfRange()) {
            errors.deviceName = cache.getCommandErrorLocaleData(
                CommandErrorsKeys.DEVICE_NAME_IS_OUT_OF_RANGE_ERROR_MSG
            );
        }
        return errors;
    }

    /**
     * Gets a boolean indicating whether the deviceId is out of range
     * + false = deviceId is included within the limits
     * + true = deviceId  [0-1023] is out of range
     * @private
     * @returns {boolean}
     * @memberof CommandParamsValidator
     */
    private isDeviceIdOutOfRange(): boolean {
        return this.data.deviceId < 0 || this.data.deviceId > 1023;
    }

    /**
     * Gets a boolean indicating whether the deviceId is out of range
     * + false = deviceId is included within the limits
     * + true = deviceId  [0-1023] is out of range
     * @private
     * @returns {boolean} 'true' if the destinationId is out of range; otherwise 'false'
     * @memberof CommandParamsValidator
     */
    private isDeviceNameOutOfRange(): boolean {
        // TODO : devicename could be padded and verify if length could be within [1-8]
        return this.data.deviceName.length < 1 || this.data.deviceName.length > 8;
    }
}
