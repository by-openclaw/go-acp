
import * as fs from 'fs';
import { CommandIdentifiers, CommandIdentifier } from '../lib/command/command-contract';
import _ from 'lodash';
import * as rimraf from 'rimraf';

console.log('test');

const destinationPath = `${__dirname}/../src/generated-command`;
console.log(`> DestinationPath : ${destinationPath}`);

const rxCommandIdentifiers = [
    // CommandIdentifiers.RX.GENERAL.CROSSPOINT_INTERROGATE_MESSAGE,
    // CommandIdentifiers.RX.GENERAL.CROSSPOINT_CONNECT_MESSAGE,
    // CommandIdentifiers.RX.GENERAL.MAINTENANCE_MESSAGE,
    //CommandIdentifiers.RX.GENERAL.DUAL_CONTROLLER_STATUS_REQUEST_MESSAGE,
    // CommandIdentifiers.RX.GENERAL.PROTECT_INTERROGATE_MESSAGE,
    // CommandIdentifiers.RX.GENERAL.PROTECT_CONNECT_MESSAGE,
    // CommandIdentifiers.RX.GENERAL.PROTECT_DIS_CONNECT_MESSAGE,
    // CommandIdentifiers.RX.GENERAL.PROTECT_DEVICE_NAME_REQUEST_MESSAGE,
    // CommandIdentifiers.RX.GENERAL.PROTECT_TALLY_DUMP_REQUEST_MESSAGE,
    // CommandIdentifiers.RX.GENERAL.CROSSPOINT_TALLY_DUMP_REQUEST_MESSAGE,
    // CommandIdentifiers.RX.GENERAL.MASTER_PROTECT_CONNECT_MESSAGE,
    //  CommandIdentifiers.RX.GENERAL.ALL_SOURCE_NAMES_REQUEST_MESSAGE,
    // CommandIdentifiers.RX.GENERAL.SINGLE_SOURCE_NAME_REQUEST_MESSAGE,
    // CommandIdentifiers.RX.GENERAL.ALL_DESTINATIONS_ASSOCIATION_NAMES_REQUEST_MESSAGE,
    // CommandIdentifiers.RX.GENERAL.SINGLE_DESTINATIONS_ASSOCIATION_NAMES_REQUEST_MESSAGE,
    // CommandIdentifiers.RX.GENERAL.CROSSPOINT_TIE_LINE_INTERROGATE_MESSAGE,
    // CommandIdentifiers.RX.GENERAL.ALL_SOURCE_ASSOCIATION_NAMES_REQUEST_MESSAGE,
    // CommandIdentifiers.RX.GENERAL.SINGLE_SOURCE_ASSOCIATION_NAMES_REQUEST_MESSAGE,
    // CommandIdentifiers.RX.GENERAL.UPDATE_NAME_REQUEST_MESSAGE,
    // CommandIdentifiers.RX.GENERAL.CROSSPOINT_CONNECT_ON_GO_GROUP_SALVO_MESSAGE,
    // CommandIdentifiers.RX.GENERAL.CROSSPOINT_GO_GROUP_SALVO_MESSAGE,
    // CommandIdentifiers.RX.GENERAL.CROSSPOINT_SALVO_GROUP_INTERROGATE_MESSAGE
];

if (fs.existsSync(destinationPath)) {
    rimraf.sync(destinationPath);
}
console.log(`> Creating DestinationPath`);
fs.mkdirSync(destinationPath);

console.log(`> Creating DestinationPath/rx folder...`);
const rxDestinationPath = `${destinationPath}/rx`;
fs.mkdirSync(rxDestinationPath);

const templatePath = `${__dirname}/template/command`;
rxCommandIdentifiers.forEach((commandIdentifier: CommandIdentifier) => {
    const paddedCommandId = _.padStart(commandIdentifier.id.toString(), 3, '0');
    const commandName = commandIdentifier.name.replace('_', '-').toLowerCase();
    const dashCommandName = commandIdentifier.name.split('_').map(s => s.toLowerCase()).join('-');
    const generatedCommandFolderPath = `${rxDestinationPath}/${paddedCommandId}-${commandName}`;
    const spaceCommandName = commandIdentifier.name.split('_').map(s => _.capitalize(s.toLowerCase())).join(' ');
    const camelCaseCommandName = commandIdentifier.name.split('_').map(s => _.capitalize(s.toLowerCase())).join('');
    console.log(`> Creating DestinationPath/rx/${paddedCommandId}-${commandName} folder...`);
    fs.mkdirSync(generatedCommandFolderPath);

    // ----------------------------------------------------------------------
    // Model (Params/ options)
    const paramsTemplateFilePath = `${templatePath}/params.template.txt`;
    let paramsTemplateFileContent = fs.readFileSync(paramsTemplateFilePath, { encoding: 'utf-8' });

    paramsTemplateFileContent = paramsTemplateFileContent.split('$$SPACE_CMD_NAME$$').join(spaceCommandName);
    paramsTemplateFileContent = paramsTemplateFileContent.split('$$CAMEL_CMD_NAME$$').join(camelCaseCommandName);

    // Params
    const paramsGeneratedModelPath = `${generatedCommandFolderPath}/params.ts`;
    fs.writeFileSync(paramsGeneratedModelPath, paramsTemplateFileContent);
    console.log(`> Creating ${paddedCommandId}-${commandName}/params.ts...`);

    const optionsTemplateFilePath = `${templatePath}/options.template.txt`;
    let optionsTemplateFileContent = fs.readFileSync(optionsTemplateFilePath, { encoding: 'utf-8' });

    // Options
    optionsTemplateFileContent = optionsTemplateFileContent.split('$$SPACE_CMD_NAME$$').join(spaceCommandName);
    optionsTemplateFileContent = optionsTemplateFileContent.split('$$CAMEL_CMD_NAME$$').join(camelCaseCommandName);

    const optionsGeneratedModelPath = `${generatedCommandFolderPath}/options.ts`;
    fs.writeFileSync(optionsGeneratedModelPath, optionsTemplateFileContent);
    console.log(`> Creating ${paddedCommandId}-${commandName}/options.ts...`);

    // ----------------------------------------------------------------------
    // Params Validator 
    const paramsValidatorTemplateFilePath = `${templatePath}/params.validator.template.txt`;
    let paramsValidatorTemplateFileContent = fs.readFileSync(paramsValidatorTemplateFilePath, { encoding: 'utf-8' });

    paramsValidatorTemplateFileContent = paramsValidatorTemplateFileContent.split('$$SPACE_CMD_NAME$$').join(spaceCommandName);
    paramsValidatorTemplateFileContent = paramsValidatorTemplateFileContent.split('$$CAMEL_CMD_NAME$$').join(camelCaseCommandName);

    const paramsGeneratedValidatorPath = `${generatedCommandFolderPath}/params.validator.ts`;
    fs.writeFileSync(paramsGeneratedValidatorPath, paramsValidatorTemplateFileContent);
    console.log(`> Creating ${paddedCommandId}-${commandName}/validator.ts...`);

    // ----------------------------------------------------------------------
    // Options Validator 
    const optionsValidatorTemplateFilePath = `${templatePath}/options.validator.template.txt`;
    let optionsValidatorTemplateFileContent = fs.readFileSync(optionsValidatorTemplateFilePath, { encoding: 'utf-8' });

    optionsValidatorTemplateFileContent = optionsValidatorTemplateFileContent.split('$$SPACE_CMD_NAME$$').join(spaceCommandName);
    optionsValidatorTemplateFileContent = optionsValidatorTemplateFileContent.split('$$CAMEL_CMD_NAME$$').join(camelCaseCommandName);

    const optionsGeneratedValidatorPath = `${generatedCommandFolderPath}/options.validator.ts`;
    fs.writeFileSync(optionsGeneratedValidatorPath, optionsValidatorTemplateFileContent);
    console.log(`> Creating ${paddedCommandId}-${commandName}/options.validator.ts...`);

    // ----------------------------------------------------------------------
    // Command
    const commandTemplateFilePath = `${templatePath}/command-options.template.txt`;
    let commandTemplateFileContent = fs.readFileSync(commandTemplateFilePath, { encoding: 'utf-8' });

    commandTemplateFileContent = commandTemplateFileContent.split('$$SPACE_CMD_NAME$$').join(spaceCommandName);
    commandTemplateFileContent = commandTemplateFileContent.split('$$CAMEL_CMD_NAME$$').join(camelCaseCommandName);

    const generatedCommandPath = `${generatedCommandFolderPath}/command.ts`;
    fs.writeFileSync(generatedCommandPath, commandTemplateFileContent);
    console.log(`> Creating ${paddedCommandId}-${commandName}/command.ts...`);

});

console.log(`> Creating DestinationPath/tx folder...`);
const txDestinationPath = `${destinationPath}/tx`;
fs.mkdirSync(txDestinationPath);
