import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { IValidator } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { ProtectDeviceNameRequestMessageCommandParams } from './params';

/**
 * ProtectDeviceNameRequestMessage command parameters validator
 *
 * @export
 * @class CommandParamsValidator
 * @implements {IValidator<ProtectDeviceNameRequestMessageCommandParams>}
 */
export class CommandParamsValidator implements IValidator<ProtectDeviceNameRequestMessageCommandParams> {
    /**
     * Creates an instance of CommandParamsValidator
     *
     * @param {ProtectDeviceNameRequestMessageCommandParams} data the command parameters
     * @memberof CommandParamsValidator
     */
    constructor(readonly data: ProtectDeviceNameRequestMessageCommandParams) {}

    /**
     * Validates the command parameter(s) and returns one error message for each invalid parameter
     *
     * @returns {(Record<string, LocaleData>)} the localized validation errors
     * @memberof CommandParamsValidator
     */
    validate(): Record<string, LocaleData> {
        const errors: Record<string, any> = {};
        const cache = LocaleDataCache.INSTANCE;

        if (this.isDeviceIdOutOfRange()) {
            errors.deviceId = cache.getCommandErrorLocaleData(CommandErrorsKeys.DEVICE_IS_OUT_OF_RANGE_ERROR_MSG);
        }
        return errors;
    }

    /**
     * Gets a boolean indicating whether the deviceId is out of range
     * + false = deviceId is included within the limits
     * + true = deviceId  [0-1023] is out of range
     *
     * @private
     * @returns {boolean} 'true' if deviceId is out of range; otherwise 'false'
     * @memberof CommandParamsValidator
     */
    private isDeviceIdOutOfRange(): boolean {
        return this.data.deviceId < 0 || this.data.deviceId > 1023;
    }
}
